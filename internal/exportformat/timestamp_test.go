package exportformat

import (
	"encoding/json"
	"testing"
	"time"
)

func TestFlexibleTimestampNumericSeconds(testContext *testing.T) {
	var timestamp FlexibleTimestamp
	if unmarshalError := json.Unmarshal([]byte(`1710000000.5`), &timestamp); unmarshalError != nil {
		testContext.Fatalf("unmarshal timestamp: %v", unmarshalError)
	}
	if !timestamp.Valid {
		testContext.Fatal("timestamp should be valid")
	}
	expectedTime := time.Unix(1710000000, 500_000_000).UTC()
	if !timestamp.Time.Equal(expectedTime) {
		testContext.Fatalf("expected %s, received %s", expectedTime, timestamp.Time)
	}
}

func TestFlexibleTimestampRFC3339(testContext *testing.T) {
	var timestamp FlexibleTimestamp
	if unmarshalError := json.Unmarshal([]byte(`"2026-07-11T12:30:00Z"`), &timestamp); unmarshalError != nil {
		testContext.Fatalf("unmarshal timestamp: %v", unmarshalError)
	}
	if timestamp.Time.Format(time.RFC3339) != "2026-07-11T12:30:00Z" {
		testContext.Fatalf("unexpected timestamp: %s", timestamp.Time)
	}
}
