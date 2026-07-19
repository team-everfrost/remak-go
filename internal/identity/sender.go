package identity

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sesv2"
	"github.com/aws/aws-sdk-go-v2/service/sesv2/types"
)

type NoopCodeSender struct{}

func (NoopCodeSender) SendVerificationCode(context.Context, string, string, string) error {
	return nil
}

type sesSender interface {
	SendEmail(context.Context, *sesv2.SendEmailInput, ...func(*sesv2.Options)) (*sesv2.SendEmailOutput, error)
}

type SESCodeSender struct {
	client sesSender
	from   string
}

func NewSESCodeSender(client sesSender, from string) *SESCodeSender {
	return &SESCodeSender{client: client, from: from}
}

func (s *SESCodeSender) SendVerificationCode(ctx context.Context, email, purpose, code string) error {
	subject := "Remak 이메일 인증 코드"
	body := fmt.Sprintf(
		"Remak %s 인증 코드는 %s 입니다. 10분 안에 입력해 주세요. 본인이 요청하지 않았다면 이 메일을 무시하세요.",
		purposeName(purpose),
		code,
	)
	_, err := s.client.SendEmail(ctx, &sesv2.SendEmailInput{
		FromEmailAddress: aws.String(s.from),
		Destination:      &types.Destination{ToAddresses: []string{email}},
		Content: &types.EmailContent{Simple: &types.Message{
			Subject: &types.Content{Data: aws.String(subject), Charset: aws.String("UTF-8")},
			Body:    &types.Body{Text: &types.Content{Data: aws.String(body), Charset: aws.String("UTF-8")}},
		}},
	})
	if err != nil {
		return fmt.Errorf("send SES verification email: %w", err)
	}
	return nil
}

func purposeName(purpose string) string {
	switch purpose {
	case "SIGNUP":
		return "회원가입"
	case "PASSWORD_RESET":
		return "비밀번호 재설정"
	case "WITHDRAW":
		return "회원 탈퇴"
	default:
		return "이메일"
	}
}
