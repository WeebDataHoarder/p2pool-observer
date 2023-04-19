package types

import (
	"fmt"
	"strconv"
	"strings"
)

type SemanticVersion uint32

func (v SemanticVersion) Major() uint16 {
	return uint16(v >> 16)
}
func (v SemanticVersion) Minor() uint16 {
	return uint16(v & 0xFFFF)
}

func (v SemanticVersion) String() string {
	if v == SemanticVersionNone {
		return "unknown"
	}
	return fmt.Sprintf("v%d.%d", v.Major(), v.Minor())
}

const SemanticVersionNone = 0

func SemanticVersionFromString(version string) SemanticVersion {
	version = strings.TrimPrefix(strings.TrimSpace(version), "v")
	parts := strings.Split(version, ".")
	if len(parts) != 2 {
		return SemanticVersionNone
	}

	major, err := strconv.ParseUint(parts[0], 10, 32)
	if err != nil {
		return SemanticVersionNone
	}

	minor, err := strconv.ParseUint(parts[1], 10, 32)
	if err != nil {
		return SemanticVersionNone
	}
	return SemanticVersion(uint32(major<<16) | uint32(minor))
}
