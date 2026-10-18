package web

import "strconv"

func atoiDefault(s string, def int) int {
	v, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return v
}
