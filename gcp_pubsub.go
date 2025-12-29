package audittrail

import (
	"context"
	"encoding/json"

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
	_, err = result.Get(ctx)
	return err
}

type gcpSubscriber struct {
	sub *pubsub.Subscription
}

func (s *gcpSubscriber) Receive(ctx context.Context, handler func(context.Context, Entry) error) error {
	return s.sub.Receive(ctx, func(ctx context.Context, msg *pubsub.Message) {
		var entry Entry
		if err := json.Unmarshal(msg.Data, &entry); err != nil {
			msg.Nack()
			return
		}
		if err := handler(ctx, entry); err != nil {
			msg.Nack()
			return
		}
		msg.Ack()
	})
}
