package api

import (
	"encoding/json"

	"brrpbrrp/internal/db"
	"brrpbrrp/internal/scraper"
)

type previewJob struct{ messageID, channelID, content string }

func (s *Server) queuePreview(message db.Message) {
	if message.ChannelID == "" || !s.Settings().LinkPreviews || len(scraper.Links(message.Content)) == 0 {
		return
	}
	select {
	case s.previewJobs <- previewJob{message.ID, message.ConversationID, message.Content}:
	default:
		_, _ = s.store.Exec(`UPDATE messages SET embed_status='skipped' WHERE id=?`, message.ID)
	}
}

func (s *Server) previewWorker() {
	defer s.workers.Done()
	for {
		select {
		case <-s.ctx.Done():
			return
		case job := <-s.previewJobs:
			if !s.Settings().LinkPreviews {
				continue
			}
			embeds := make([]scraper.Embed, 0)
			for _, link := range scraper.Links(job.content) {
				embed, err := s.scraper.Fetch(s.ctx, link)
				if err == nil {
					embeds = append(embeds, embed)
				}
			}
			if s.ctx.Err() != nil {
				return
			}
			if !s.Settings().LinkPreviews {
				continue
			}
			encoded, _ := json.Marshal(embeds)
			result, err := s.store.ExecContext(s.ctx, `UPDATE messages SET embed_json=?,embed_status='done' WHERE id=? AND content=?`, string(encoded), job.messageID, job.content)
			if err != nil {
				s.log.Warn("link preview could not be saved")
				continue
			}
			count, _ := result.RowsAffected()
			if count > 0 {
				s.publishToConversation(s.ctx, job.channelID, "message.embed_updated", map[string]string{"id": job.messageID, "channel_id": job.channelID, "conversation_id": job.channelID})
			}
		}
	}
}
