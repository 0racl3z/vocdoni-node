package util

import (
	"strconv"
	"strings"
)

func Hex2int64(hexStr string) int64 {
	// remove 0x suffix if found in the input string
	cleaned := strings.Replace(hexStr, "0x", "", -1)

	// base 16 for hexadecimal
	result, err := strconv.ParseUint(cleaned, 16, 64)
	if err != nil {
		panic(err)
	}
	return int64(result)
}

func TrimHex(hexStr string) string {
	return strings.TrimPrefix(hexStr, "0x")
}
