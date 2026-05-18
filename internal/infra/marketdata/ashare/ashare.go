package ashare

import (
	"fmt"
	"regexp"
	"strings"
)

const (
	BoardSHMain  = "sh_main"
	BoardSZMain  = "sz_main"
	BoardBJ      = "bj"
	BoardSTAR    = "star"
	BoardChiNext = "chinext"
	BoardETFLOF  = "etf_lof"
)

var codePattern = regexp.MustCompile(`^\d{6}$`)

var BoardLabels = map[string]string{
	BoardSHMain:  "SSE Main Board",
	BoardSZMain:  "SZSE Main Board",
	BoardBJ:      "BSE",
	BoardSTAR:    "STAR Market",
	BoardChiNext: "ChiNext",
	BoardETFLOF:  "Exchange-traded ETF/LOF",
}

type Listing struct {
	Code     string
	Board    string
	Exchange string
}

type RiskConfig interface {
	GetAllowSHMain() bool
	GetAllowSZMain() bool
	GetAllowBJ() bool
	GetAllowSTAR() bool
	GetAllowChiNext() bool
	GetAllowETFLOF() bool
}

func Detect(value string) *Listing {
	code := strings.TrimSpace(strings.ToUpper(value))
	code = strings.TrimPrefix(code, "SH")
	code = strings.TrimPrefix(code, "SZ")
	code = strings.TrimPrefix(code, "BJ")
	if !codePattern.MatchString(code) {
		return nil
	}
	switch {
	case strings.HasPrefix(code, "5"):
		return &Listing{Code: code, Board: BoardETFLOF, Exchange: "SH"}
	case strings.HasPrefix(code, "15") || strings.HasPrefix(code, "16"):
		return &Listing{Code: code, Board: BoardETFLOF, Exchange: "SZ"}
	case strings.HasPrefix(code, "688"):
		return &Listing{Code: code, Board: BoardSTAR, Exchange: "SH"}
	case strings.HasPrefix(code, "600") || strings.HasPrefix(code, "601") || strings.HasPrefix(code, "603") || strings.HasPrefix(code, "605"):
		return &Listing{Code: code, Board: BoardSHMain, Exchange: "SH"}
	case strings.HasPrefix(code, "300") || strings.HasPrefix(code, "301"):
		return &Listing{Code: code, Board: BoardChiNext, Exchange: "SZ"}
	case strings.HasPrefix(code, "000") || strings.HasPrefix(code, "001") || strings.HasPrefix(code, "002") || strings.HasPrefix(code, "003"):
		return &Listing{Code: code, Board: BoardSZMain, Exchange: "SZ"}
	case strings.HasPrefix(code, "4") || strings.HasPrefix(code, "8") || strings.HasPrefix(code, "92"):
		return &Listing{Code: code, Board: BoardBJ, Exchange: "BJ"}
	default:
		return nil
	}
}

func EnsureCode(value string) (string, error) {
	listing := Detect(value)
	if listing == nil {
		return "", fmt.Errorf("only China-listed 6-digit stocks, ETFs, and LOFs are supported")
	}
	return listing.Code, nil
}

func BoardLabel(board string) string {
	if label, ok := BoardLabels[board]; ok {
		return label
	}
	return board
}

func AllowsBoard(config RiskConfig, board string) bool {
	switch board {
	case BoardSHMain:
		return config.GetAllowSHMain()
	case BoardSZMain:
		return config.GetAllowSZMain()
	case BoardBJ:
		return config.GetAllowBJ()
	case BoardSTAR:
		return config.GetAllowSTAR()
	case BoardChiNext:
		return config.GetAllowChiNext()
	case BoardETFLOF:
		return config.GetAllowETFLOF()
	default:
		return false
	}
}

func EnabledBoards(config RiskConfig) []string {
	boards := []string{BoardSHMain, BoardSZMain, BoardBJ, BoardSTAR, BoardChiNext, BoardETFLOF}
	out := make([]string, 0, len(boards))
	for _, board := range boards {
		if AllowsBoard(config, board) {
			out = append(out, board)
		}
	}
	return out
}

func ValidateAgainstRiskConfig(config RiskConfig, value string) (*Listing, error) {
	listing := Detect(value)
	if listing == nil {
		return nil, fmt.Errorf("only China-listed 6-digit stocks, ETFs, and LOFs are supported")
	}
	if AllowsBoard(config, listing.Board) {
		return listing, nil
	}
	enabled := EnabledBoards(config)
	labels := make([]string, 0, len(enabled))
	for _, board := range enabled {
		labels = append(labels, BoardLabel(board))
	}
	enabledText := "no boards enabled"
	if len(labels) > 0 {
		enabledText = strings.Join(labels, ", ")
	}
	return nil, fmt.Errorf("%s belongs to %s, but current risk config allows %s", listing.Code, BoardLabel(listing.Board), enabledText)
}
