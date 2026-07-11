package service

import (
	"context"
	"encoding/json"
	"strconv"
)

var extraConcurrencyRuntimeSettingKeys = []string{
	SettingKeyExtraConcurrencyEnabled,
	SettingKeyExtraConcurrencyWaitTimeoutSeconds,
	SettingKeyExtraConcurrencyReservePercent,
	SettingKeyExtraConcurrencyMinReservedSlots,
	SettingKeyExtraConcurrencyPlatformReserves,
}

const extraConcurrencyRuntimeCacheKey = "extra_concurrency_runtime"

type ExtraConcurrencyRuntimeSettings struct {
	Enabled            bool
	WaitTimeoutSeconds int
	ReservePercent     float64
	MinReservedSlots   int
	PlatformReserves   map[string]ExtraConcurrencyPlatformReserve
}

func (s *SettingService) GetExtraConcurrencyRuntimeSettings(ctx context.Context) ExtraConcurrencyRuntimeSettings {
	fallback := disabledExtraConcurrencyRuntimeSettings()
	if s == nil || s.settingRepo == nil {
		return fallback
	}
	if cached := s.loadExtraConcurrencyRuntimeSettings(); cached != nil {
		return cloneExtraConcurrencyRuntimeSettings(*cached)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	result, _, _ := s.extraConcurrencyRuntimeSF.Do(extraConcurrencyRuntimeCacheKey, func() (any, error) {
		if cached := s.loadExtraConcurrencyRuntimeSettings(); cached != nil {
			return cached, nil
		}
		values, err := s.settingRepo.GetMultiple(ctx, extraConcurrencyRuntimeSettingKeys)
		snapshot := fallback
		if err == nil {
			snapshot = parseExtraConcurrencyRuntimeSettings(values)
		}
		cached := cloneExtraConcurrencyRuntimeSettings(snapshot)
		s.extraConcurrencyRuntimeCache.Store(&cached)
		return &cached, nil
	})
	cached, ok := result.(*ExtraConcurrencyRuntimeSettings)
	if !ok || cached == nil {
		return fallback
	}
	return cloneExtraConcurrencyRuntimeSettings(*cached)
}

func (s *SettingService) loadExtraConcurrencyRuntimeSettings() *ExtraConcurrencyRuntimeSettings {
	cached, _ := s.extraConcurrencyRuntimeCache.Load().(*ExtraConcurrencyRuntimeSettings)
	return cached
}

func parseExtraConcurrencyRuntimeSettings(values map[string]string) ExtraConcurrencyRuntimeSettings {
	fallback := disabledExtraConcurrencyRuntimeSettings()
	for _, key := range extraConcurrencyRuntimeSettingKeys {
		if _, ok := values[key]; !ok {
			return fallback
		}
	}

	enabled, err := strconv.ParseBool(values[SettingKeyExtraConcurrencyEnabled])
	if err != nil {
		return fallback
	}
	waitTimeoutSeconds, err := strconv.Atoi(values[SettingKeyExtraConcurrencyWaitTimeoutSeconds])
	if err != nil {
		return fallback
	}
	reservePercent, err := strconv.ParseFloat(values[SettingKeyExtraConcurrencyReservePercent], 64)
	if err != nil {
		return fallback
	}
	minReservedSlots, err := strconv.Atoi(values[SettingKeyExtraConcurrencyMinReservedSlots])
	if err != nil {
		return fallback
	}
	var platformReserves map[string]ExtraConcurrencyPlatformReserve
	if err := json.Unmarshal([]byte(values[SettingKeyExtraConcurrencyPlatformReserves]), &platformReserves); err != nil || platformReserves == nil {
		return fallback
	}

	settings := &SystemSettings{
		ExtraConcurrencyWaitTimeoutSeconds: waitTimeoutSeconds,
		ExtraConcurrencyReservePercent:     reservePercent,
		ExtraConcurrencyMinReservedSlots:   minReservedSlots,
		ExtraConcurrencyPlatformReserves:   platformReserves,
	}
	if err := validateExtraConcurrencySettings(settings); err != nil {
		return fallback
	}

	return ExtraConcurrencyRuntimeSettings{
		Enabled:            enabled,
		WaitTimeoutSeconds: waitTimeoutSeconds,
		ReservePercent:     reservePercent,
		MinReservedSlots:   minReservedSlots,
		PlatformReserves:   cloneExtraConcurrencyPlatformReserves(platformReserves),
	}
}

func disabledExtraConcurrencyRuntimeSettings() ExtraConcurrencyRuntimeSettings {
	return ExtraConcurrencyRuntimeSettings{
		Enabled:            false,
		WaitTimeoutSeconds: 30,
		ReservePercent:     10,
		MinReservedSlots:   1,
		PlatformReserves:   map[string]ExtraConcurrencyPlatformReserve{},
	}
}

func cloneExtraConcurrencyPlatformReserves(source map[string]ExtraConcurrencyPlatformReserve) map[string]ExtraConcurrencyPlatformReserve {
	cloned := make(map[string]ExtraConcurrencyPlatformReserve, len(source))
	for platform, reserve := range source {
		copy := reserve
		if reserve.ReservePercent != nil {
			value := *reserve.ReservePercent
			copy.ReservePercent = &value
		}
		if reserve.MinReservedSlots != nil {
			value := *reserve.MinReservedSlots
			copy.MinReservedSlots = &value
		}
		cloned[platform] = copy
	}
	return cloned
}

func cloneExtraConcurrencyRuntimeSettings(source ExtraConcurrencyRuntimeSettings) ExtraConcurrencyRuntimeSettings {
	copy := source
	copy.PlatformReserves = cloneExtraConcurrencyPlatformReserves(source.PlatformReserves)
	return copy
}
