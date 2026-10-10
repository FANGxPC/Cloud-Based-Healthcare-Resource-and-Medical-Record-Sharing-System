// Package notify publishes alerts to the SNS topic, which emails its subscribers.
//
// Rule: messages contain IDs and counts only, never patient names or clinical details (no PHI),
// because email is not a secure channel. Every message carries a "hospitalId" message
// attribute, so an email subscription can use an SNS filter policy to receive only one hospital's alerts.
package notify

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	"github.com/aws/aws-sdk-go-v2/service/sns/types"
)

// Publisher sends one notification. Tests use a fake.
type Publisher interface {
	Publish(ctx context.Context, subject, message, hospitalID, kind string) error
}

// SNS publishes to a topic.
type SNS struct {
	Client   *sns.Client
	TopicArn string
}

func (s SNS) Publish(ctx context.Context, subject, message, hospitalID, kind string) error {
	_, err := s.Client.Publish(ctx, &sns.PublishInput{
		TopicArn: aws.String(s.TopicArn),
		Subject:  aws.String(truncate(subject, 100)), // SNS email subject limit
		Message:  aws.String(message),
		MessageAttributes: map[string]types.MessageAttributeValue{
			"hospitalId": {DataType: aws.String("String"), StringValue: aws.String(hospitalID)},
			"kind":       {DataType: aws.String("String"), StringValue: aws.String(kind)},
		},
	})
	return err
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
