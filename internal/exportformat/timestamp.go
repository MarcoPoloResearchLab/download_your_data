package exportformat

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

type FlexibleTimestamp struct {
	Time  time.Time
	Valid bool
}

func (timestamp *FlexibleTimestamp) UnmarshalJSON(data []byte) error {
	trimmedData := bytes.TrimSpace(data)
	if bytes.Equal(trimmedData, []byte("null")) || len(trimmedData) == 0 {
		timestamp.Valid = false
		return nil
	}

	if trimmedData[0] == '"' {
		var stringValue string
		if err := json.Unmarshal(trimmedData, &stringValue); err != nil {
			return fmt.Errorf("decode timestamp string: %w", err)
		}
		return timestamp.parseString(stringValue)
	}

	var numericValue float64
	if err := json.Unmarshal(trimmedData, &numericValue); err != nil {
		return fmt.Errorf("decode numeric timestamp: %w", err)
	}
	timestamp.setNumeric(numericValue)
	return nil
}

func (timestamp *FlexibleTimestamp) parseString(value string) error {
	trimmedValue := strings.TrimSpace(value)
	if trimmedValue == "" {
		timestamp.Valid = false
		return nil
	}

	numericValue, numericError := strconv.ParseFloat(trimmedValue, 64)
	if numericError == nil {
		timestamp.setNumeric(numericValue)
		return nil
	}

	parsedTime, parseError := time.Parse(time.RFC3339Nano, trimmedValue)
	if parseError != nil {
		return fmt.Errorf("unsupported timestamp %q", trimmedValue)
	}
	timestamp.Time = parsedTime.UTC()
	timestamp.Valid = true
	return nil
}

func (timestamp *FlexibleTimestamp) setNumeric(value float64) {
	if value == 0 {
		timestamp.Valid = false
		return
	}

	var seconds int64
	var nanoseconds int64
	if value > 100_000_000_000 {
		milliseconds := int64(value)
		seconds = milliseconds / 1000
		nanoseconds = (milliseconds % 1000) * int64(time.Millisecond)
	} else {
		seconds = int64(value)
		fractionalSeconds := value - float64(seconds)
		nanoseconds = int64(fractionalSeconds * float64(time.Second))
	}

	timestamp.Time = time.Unix(seconds, nanoseconds).UTC()
	timestamp.Valid = true
}

func (timestamp FlexibleTimestamp) Pointer() *time.Time {
	if !timestamp.Valid {
		return nil
	}
	copiedTime := timestamp.Time
	return &copiedTime
}
