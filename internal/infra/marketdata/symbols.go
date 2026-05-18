package marketdata

import "github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/marketdata/ashare"

func EnsureAShareCode(value string) (string, error) {
	return ashare.EnsureCode(value)
}

func InferExchange(code string) string {
	if listing := ashare.Detect(code); listing != nil {
		return listing.Exchange
	}
	return ""
}

func Board(code string) string {
	if listing := ashare.Detect(code); listing != nil {
		return listing.Board
	}
	return ""
}
