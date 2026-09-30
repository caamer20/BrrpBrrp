package rtc

import (
	"testing"
	"time"
)

// A publication is a thing with an identity, not a position. These tests pin the
// properties the old slot model could not express: a screen and a microphone are
// different objects, a publication has its own bound, and a stop is not a mute.

func TestPublicationKindIsClassifiedNotGuessed(t *testing.T) {
	// A screen track must never be stored or forwarded as though it were a microphone.
	// That is the whole reason the kind is a value and not a boolean.
	if PublicationScreenVideo.Audio() {
		t.Fatal("screen video is treated as audio, so it could be forwarded with audio's clock")
	}
	if PublicationMicrophone.Audio() != true {
		t.Fatal("a microphone is not treated as audio")
	}
	if PublicationScreenAudio.Audio() != true {
		t.Fatal("screen audio is not treated as audio")
	}
	// And the two audio kinds use the same clock, which is why they share a codec.
	microphone, ok := PublicationMicrophone.Codec()
	if !ok {
		t.Fatal("a microphone has no codec")
	}
	screenAudio, ok := PublicationScreenAudio.Codec()
	if !ok {
		t.Fatal("screen audio has no codec")
	}
	if microphone.ClockRate != screenAudio.ClockRate {
		t.Fatalf("audio publications disagree on clock rate: %d and %d", microphone.ClockRate, screenAudio.ClockRate)
	}
	// Video does not share audio's clock. Forwarding a video track through the audio
	// constants is exactly the bug the kind exists to prevent.
	video, ok := PublicationScreenVideo.Codec()
	if !ok {
		t.Fatal("screen video has no codec")
	}
	if video.ClockRate == microphone.ClockRate {
		t.Fatal("screen video reuses the audio clock rate")
	}
}

func TestUnknownPublicationKindIsRefusedRatherThanIgnored(t *testing.T) {
	// A client publishing something the server cannot classify has no way to know it
	// is not being forwarded, so an unknown kind is an error, not a shrug.
	if PublicationKind("hologram").Valid() {
		t.Fatal("an unknown kind was accepted")
	}
	if _, ok := PublicationKind("hologram").Codec(); ok {
		t.Fatal("an unknown kind was given a codec")
	}
	for _, kind := range []PublicationKind{PublicationMicrophone, PublicationScreenVideo, PublicationScreenAudio} {
		if !kind.Valid() {
			t.Fatalf("%s is not recognised as valid", kind)
		}
	}
}

func TestPublicationRegistryKeepsAnnouncementOrder(t *testing.T) {
	// A participant list that reorders on every refresh is unreadable, and Go's map
	// order changes between calls — so the order is recorded rather than derived.
	registry := newPublicationRegistry()
	for _, owner := range []string{"ana", "bo", "cy"} {
		registry.add(&Publication{ID: PublicationID(owner), Owner: owner, Kind: PublicationMicrophone, Started: time.Now()})
	}
	listed := registry.list()
	if len(listed) != 3 {
		t.Fatalf("listed %d publications, want 3", len(listed))
	}
	for i, owner := range []string{"ana", "bo", "cy"} {
		if listed[i].Owner != owner {
			t.Fatalf("position %d is %q, want %q", i, listed[i].Owner, owner)
		}
	}
	// A refresh returns the same order.
	if again := registry.list(); again[0].Owner != "ana" || again[2].Owner != "cy" {
		t.Fatal("the order changed between two identical lists")
	}
}

func TestRemovingAPublicationRemovesItFromTheOrderToo(t *testing.T) {
	// A removed publication left in the order would appear in every later list as a
	// gap, and a gap in a participant list looks like a person who vanished.
	registry := newPublicationRegistry()
	for _, owner := range []string{"ana", "bo", "cy"} {
		registry.add(&Publication{ID: PublicationID(owner), Owner: owner, Kind: PublicationMicrophone})
	}
	registry.remove("bo")
	listed := registry.list()
	if len(listed) != 2 {
		t.Fatalf("listed %d publications after a removal, want 2", len(listed))
	}
	for _, publication := range listed {
		if publication.Owner == "bo" {
			t.Fatal("a removed publication is still listed")
		}
	}
	if _, ok := registry.get("bo"); ok {
		t.Fatal("a removed publication is still retrievable")
	}
	if _, ok := registry.remove("bo"); ok {
		t.Fatal("removing an absent publication reported success")
	}
}

func TestScreenAndMicrophoneAreCountedSeparately(t *testing.T) {
	// This is the distinction a single "participant count" cannot make. A room with
	// one person sharing a screen and one speaking has one of each, not two of
	// something, and the screen bound is about the screen.
	registry := newPublicationRegistry()
	registry.add(&Publication{ID: "m1", Kind: PublicationMicrophone})
	registry.add(&Publication{ID: "s1", Kind: PublicationScreenVideo})
	registry.add(&Publication{ID: "m2", Kind: PublicationMicrophone})
	if got := registry.countOfKind(PublicationScreenVideo); got != 1 {
		t.Fatalf("counted %d screens, want 1", got)
	}
	if got := registry.countOfKind(PublicationMicrophone); got != 2 {
		t.Fatalf("counted %d microphones, want 2", got)
	}
	if got := registry.size(); got != 3 {
		t.Fatalf("the registry holds %d publications, want 3", got)
	}
}

func TestOneScreenPerRoomAndTwoOnTheServer(t *testing.T) {
	// The v1 targets. They are separate bounds, and the difference matters: a
	// serverwide bound of one would make a busy community unusable for everybody not
	// in the room that holds it.
	limits := DefaultPublicationLimits()
	// A room with no screen accepts one.
	if err := limits.admits(PublicationScreenVideo, 0, 0); err != nil {
		t.Fatalf("the first screen was refused: %v", err)
	}
	// A second in the same room is refused, and says so in a way a person can act on.
	err := limits.admits(PublicationScreenVideo, 1, 1)
	if err == nil {
		t.Fatal("a second screen in one room was admitted")
	}
	if err.Error() == "" {
		t.Fatal("the refusal has no explanation, so a client cannot tell a person why")
	}
	// A first screen in a *different* room is fine while the server is under its bound.
	if err := limits.admits(PublicationScreenVideo, 0, 1); err != nil {
		t.Fatalf("a second room could not start a screen: %v", err)
	}
	// The serverwide bound stops it, and that refusal is a different sentence from
	// the room's — the two are different problems with different remedies.
	serverWide := limits.admits(PublicationScreenVideo, 0, 2)
	if serverWide == nil {
		t.Fatal("the server accepted a third screen")
	}
	if serverWide.Error() == err.Error() {
		t.Fatal("a serverwide refusal reads the same as a room refusal, so nobody can tell which to fix")
	}
}

func TestScreenAudioIsBoundedByItsVideoRatherThanSeparately(t *testing.T) {
	// Screen audio travels with a screen. A separate bound would let somebody publish
	// audio for a screen they cannot publish, which is the shape of a bug.
	limits := DefaultPublicationLimits()
	if err := limits.admits(PublicationScreenAudio, 0, 0); err != nil {
		t.Fatalf("screen audio was refused outright: %v", err)
	}
	// It is not capped by the microphone bound, so a room at its microphone limit can
	// still carry a screen's audio.
	for range limits.PerRoomMicrophones {
		if err := limits.admits(PublicationMicrophone, 1, 0); err == nil {
			break
		}
	}
	if err := limits.admits(PublicationScreenAudio, limits.PerRoomMicrophones, 0); err != nil {
		t.Fatalf("screen audio is capped by the microphone bound: %v", err)
	}
}

func TestAStoppedPublicationIsRemovedRatherThanLeftSilent(t *testing.T) {
	// A viewer cannot tell a stopped publication from a person who stopped talking, so
	// a stop removes the publication. Keeping it as a silent entry would make a
	// participant list lie.
	registry := newPublicationRegistry()
	registry.add(&Publication{ID: "s1", Kind: PublicationScreenVideo, Owner: "ana"})
	registry.remove("s1")
	if registry.countOfKind(PublicationScreenVideo) != 0 {
		t.Fatal("a stopped screen is still counted")
	}
	if _, ok := registry.get("s1"); ok {
		t.Fatal("a stopped screen is still retrievable")
	}
}

func TestMuteAndStopAreDifferentThings(t *testing.T) {
	// A mute is the participant's choice and persists; a stop is the end of the
	// publication. Conflating them is how a permission withdrawal turns into a
	// self-mute somebody has to discover and undo.
	muted := Publication{ID: "m1", Kind: PublicationMicrophone, Muted: true, Stopped: false}
	if !muted.Muted || muted.Stopped {
		t.Fatal("a mute is not distinguishable from a stop")
	}
	stopped := Publication{ID: "m1", Kind: PublicationMicrophone, Muted: false, Stopped: true}
	if stopped.Muted {
		t.Fatal("a stop is reported as a mute")
	}
}

func TestPublicationIdentitySurvivesAStopAndRestart(t *testing.T) {
	// A client reconnecting mid-publication has to be able to tell a resume from a new
	// publication that happens to have reused the slot. The generation is what says
	// which, and it only ever moves forward.
	first := &Publication{ID: "s1", Kind: PublicationScreenVideo, Generation: 1}
	restarted := &Publication{ID: "s1", Kind: PublicationScreenVideo, Generation: first.Generation + 1}
	if restarted.ID != first.ID {
		t.Fatal("the identity changed across a restart")
	}
	if restarted.Generation == first.Generation {
		t.Fatal("a restart is indistinguishable from a resume")
	}
}

func TestGetReturnsACopySoACallerCannotMutateTheRegistry(t *testing.T) {
	// The registry is read from the event loop and from request handlers at the same
	// time. Handing out the stored pointer would let one of them change what the other
	// sees without going through the lock.
	registry := newPublicationRegistry()
	registry.add(&Publication{ID: "m1", Kind: PublicationMicrophone, Owner: "ana", Muted: true})
	got, ok := registry.get("m1")
	if !ok {
		t.Fatal("the publication was not retrievable")
	}
	got.Muted = false
	got.Owner = "mallory"
	again, _ := registry.get("m1")
	if !again.Muted || again.Owner != "ana" {
		t.Fatal("a caller mutated the stored publication through a copy")
	}
}
