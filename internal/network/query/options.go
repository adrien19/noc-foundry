package query

import (
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/adrien19/noc-foundry/internal/network/models"
	"github.com/adrien19/noc-foundry/internal/network/profiles"
)

// ExecuteOptions carries runtime controls for profile-routed operations.
type ExecuteOptions struct {
	Params               map[string]any
	LimitOverride        *LimitOverride
	AllowUnsafeFullFetch bool
}

// LimitOverride lets trusted callers narrow sidecar safety limits.
type LimitOverride struct {
	Count int
	Bytes int
}

func appendQualityWarning(q *models.QualityMeta, warning string) {
	if warning == "" {
		return
	}
	for _, existing := range q.Warnings {
		if existing == warning {
			return
		}
	}
	q.Warnings = append(q.Warnings, warning)
}

func payloadLength(payload any) int {
	if payload == nil {
		return 0
	}
	v := reflect.ValueOf(payload)
	if v.Kind() == reflect.Slice || v.Kind() == reflect.Array {
		return v.Len()
	}
	return 1
}

func truncatePayload(payload any, limit int) (any, bool) {
	if limit <= 0 || payload == nil {
		return payload, false
	}
	v := reflect.ValueOf(payload)
	if v.Kind() != reflect.Slice && v.Kind() != reflect.Array {
		return payload, false
	}
	if v.Len() <= limit {
		return payload, false
	}
	return v.Slice(0, limit).Interface(), true
}

func payloadBytes(payload any) int {
	raw, err := json.Marshal(payload)
	if err != nil {
		return 0
	}
	return len(raw)
}

func enforceLimits(payload any, quality models.QualityMeta, limits *profiles.OperationLimits, override *LimitOverride) (any, models.QualityMeta) {
	if limits == nil && override == nil {
		return payload, quality
	}
	countLimit := 0
	byteLimit := 0
	if limits != nil {
		countLimit = limits.DefaultCount
		if countLimit == 0 {
			countLimit = limits.MaxCount
		}
		byteLimit = limits.MaxBytes
	}
	if override != nil {
		if override.Count > 0 {
			countLimit = override.Count
		}
		if override.Bytes > 0 {
			byteLimit = override.Bytes
		}
	}
	if limits != nil && limits.MaxCount > 0 && countLimit > limits.MaxCount {
		countLimit = limits.MaxCount
	}
	if countLimit > 0 {
		before := payloadLength(payload)
		var truncated bool
		payload, truncated = truncatePayload(payload, countLimit)
		if truncated {
			appendQualityWarning(&quality, fmt.Sprintf("payload truncated from %d records to safety limit %d", before, countLimit))
		}
	}
	if byteLimit > 0 && payloadBytes(payload) > byteLimit {
		appendQualityWarning(&quality, fmt.Sprintf("payload size exceeds safety limit %d bytes; caller should request a narrower source-side filter", byteLimit))
	}
	return payload, quality
}
