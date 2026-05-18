package mfa

import (
	"math"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
)

const (
	defaultIssuer = "image-bed"
	totpPeriod    = 30
	totpSkew      = 1
)

// GenerateTOTPSecret creates a TOTP secret and otpauth URI for frontend QR rendering.
func GenerateTOTPSecret(username string) (secret string, uri string, err error) {
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      defaultIssuer,
		AccountName: username,
		Algorithm:   otp.AlgorithmSHA1,
		Digits:      otp.DigitsSix,
		Period:      totpPeriod,
	})
	if err != nil {
		return "", "", err
	}
	return key.Secret(), key.URL(), nil
}

// ValidateTOTPCode verifies a code and returns the matched time-step.
func ValidateTOTPCode(secret, code string, now time.Time) (int64, bool) {
	if secret == "" || code == "" {
		return 0, false
	}

	opts := totp.ValidateOpts{
		Period:    totpPeriod,
		Skew:      0,
		Digits:    otp.DigitsSix,
		Algorithm: otp.AlgorithmSHA1,
	}

	currentStep := int64(math.Floor(float64(now.UTC().Unix()) / float64(totpPeriod)))
	for offset := int64(-totpSkew); offset <= totpSkew; offset++ {
		step := currentStep + offset
		if step < 0 {
			continue
		}
		stepTime := time.Unix(step*totpPeriod, 0).UTC()
		ok, err := totp.ValidateCustom(code, secret, stepTime, opts)
		if err == nil && ok {
			return step, true
		}
	}
	return 0, false
}
