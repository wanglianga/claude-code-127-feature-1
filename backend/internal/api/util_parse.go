package api

import (
	"strconv"
	"strings"
)

// parseEU 从 "EU42.5" 解析 42.5
func parseEU(s string, out *float64) error {
	s = strings.TrimSpace(strings.TrimPrefix(strings.ToUpper(s), "EU"))
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return err
	}
	*out = v
	return nil
}

// parseCM 从 "145cm" 解析 145
func parseCM(s string, out *float64) error {
	s = strings.TrimSpace(strings.ToLower(s))
	s = strings.TrimSuffix(s, "cm")
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return err
	}
	*out = v
	return nil
}
