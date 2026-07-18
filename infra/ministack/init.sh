#!/bin/sh
set -eu

# Pin every CLI call to the emulator. MiniStack injects these values into init
# subprocesses, but being explicit also works across older AWS CLI v1 images.
export AWS_ACCESS_KEY_ID=test
export AWS_SECRET_ACCESS_KEY=test
export AWS_DEFAULT_REGION=ap-northeast-2

aws_local() {
  aws --endpoint-url=http://localhost:4566 --region "$AWS_DEFAULT_REGION" "$@"
}

aws_local s3api create-bucket --bucket remak-artifacts --create-bucket-configuration LocationConstraint=ap-northeast-2 2>/dev/null || true
aws_local s3api create-bucket --bucket remak-documents --create-bucket-configuration LocationConstraint=ap-northeast-2 2>/dev/null || true

request_dlq_url="$(aws_local sqs create-queue --queue-name remak-scrape-request-dlq --query QueueUrl --output text)"
request_dlq_arn="$(aws_local sqs get-queue-attributes --queue-url "$request_dlq_url" --attribute-names QueueArn --query 'Attributes.QueueArn' --output text)"
request_queue_url="$(aws_local sqs create-queue --queue-name remak-scrape-request --attributes VisibilityTimeout=660 --query QueueUrl --output text)"
request_redrive_input="$(python3 -c 'import json,sys; print(json.dumps({"QueueUrl":sys.argv[1],"Attributes":{"RedrivePolicy":json.dumps({"deadLetterTargetArn":sys.argv[2],"maxReceiveCount":"3"})}}))' "$request_queue_url" "$request_dlq_arn")"
aws_local sqs set-queue-attributes --cli-input-json "$request_redrive_input"

result_dlq_url="$(aws_local sqs create-queue --queue-name remak-scrape-result-dlq --query QueueUrl --output text)"
result_dlq_arn="$(aws_local sqs get-queue-attributes --queue-url "$result_dlq_url" --attribute-names QueueArn --query 'Attributes.QueueArn' --output text)"
result_queue_url="$(aws_local sqs create-queue --queue-name remak-scrape-result --attributes VisibilityTimeout=120 --query QueueUrl --output text)"
result_redrive_input="$(python3 -c 'import json,sys; print(json.dumps({"QueueUrl":sys.argv[1],"Attributes":{"RedrivePolicy":json.dumps({"deadLetterTargetArn":sys.argv[2],"maxReceiveCount":"5"})}}))' "$result_queue_url" "$result_dlq_arn")"
aws_local sqs set-queue-attributes --cli-input-json "$result_redrive_input"
