package app

// Version identifies this build. It is one of three places that must agree —
// internal/app/version.go, web/package.json and the default in
// scripts/release.go — and TestVersionIdentityAgrees fails when they drift,
// because a release must be identifiable as one thing everywhere it is
// published (F-15 in the completion review).
var Version = "0.4.0-alpha.7"
