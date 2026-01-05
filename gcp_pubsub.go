package audittrail

import (
	"context"
	"encoding/json"
	"log"

	"cloud.google.com/go/pubsub"
)

type gcpPublisher struct {
	topic *pubsub.Topic
}

func (p *gcpPublisher) Publish(ctx context.Context, entry Entry) error {
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}

	result := p.topic.Publish(ctx, &pubsub.Message{Data: data})

	// Handle publish result asynchronously to avoid blocking
	go func() {
		if _, err := result.Get(context.Background()); err != nil {
			log.Printf("audittrail: publish to pubsub failed for entry %s: %v", entry.ID, err)
		}
	}()

	return nil
}

type gcpSubscriber struct {
	sub *pubsub.Subscription
}

func (s *gcpSubscriber) Receive(ctx context.Context, handler func(context.Context, Entry) error) error {
	return s.sub.Receive(ctx, func(ctx context.Context, msg *pubsub.Message) {
		var entry Entry
		if err := json.Unmarshal(msg.Data, &entry); err != nil {
			log.Printf("audittrail: failed to unmarshal pubsub message: %v, data: %s", err, string(msg.Data))
			msg.Nack()
			return
		}
		if err := handler(ctx, entry); err != nil {
			log.Printf("audittrail: handler failed for entry %s: %v", entry.ID, err)
			msg.Nack()
			return
		}
		msg.Ack()
	})
}
