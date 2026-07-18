package awsx

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/sesv2"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
)

type Clients struct {
	SQS *sqs.Client
	S3  *s3.Client
	SES *sesv2.Client
}

func New(ctx context.Context, region, endpoint string) (Clients, error) {
	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
	if err != nil {
		return Clients{}, fmt.Errorf("load AWS configuration: %w", err)
	}
	sqsClient := sqs.NewFromConfig(cfg, func(options *sqs.Options) {
		if endpoint != "" {
			options.BaseEndpoint = aws.String(endpoint)
		}
	})
	s3Client := s3.NewFromConfig(cfg, func(options *s3.Options) {
		if endpoint != "" {
			options.BaseEndpoint = aws.String(endpoint)
			options.UsePathStyle = true
		}
	})
	sesClient := sesv2.NewFromConfig(cfg, func(options *sesv2.Options) {
		if endpoint != "" {
			options.BaseEndpoint = aws.String(endpoint)
		}
	})
	return Clients{SQS: sqsClient, S3: s3Client, SES: sesClient}, nil
}
