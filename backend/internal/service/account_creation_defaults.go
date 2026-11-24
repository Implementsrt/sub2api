package service

import infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"

// Creation uses explicit settings, never probe snapshots copied through extra.
// Defaults belong to the personal UI; omitted API fields preserve old behavior.
func applyCreationBillingSettings(account *Account, input *CreateAccountInput) error {
	probe := input.ProbeEnabled != nil && *input.ProbeEnabled
	rateSync := input.RateSyncEnabled != nil && *input.RateSyncEnabled
	groupSync := input.GroupSyncEnabled != nil && *input.GroupSyncEnabled
	if input.ProbeEnabled != nil && !probe {
		if groupSync {
			return ErrUpstreamBillingGroupSyncRequiresProbe
		}
		if rateSync {
			return infraerrors.BadRequest("UPSTREAM_BILLING_RATE_SYNC_REQUIRES_PROBE", "upstream billing rate sync requires upstream billing probe")
		}
	}
	if !probe && !rateSync && !groupSync {
		return nil
	}
	if !isUpstreamBillingProbeAccount(account) {
		return ErrUpstreamBillingProbeAccountInvalid
	}
	if account.Extra == nil {
		account.Extra = make(map[string]any)
	}
	account.Extra[UpstreamBillingProbeEnabledExtraKey] = true
	account.Extra[UpstreamBillingRateSyncEnabledExtraKey] = rateSync
	if groupSync {
		account.Extra[UpstreamBillingGroupSyncEnabledExtraKey] = true
	}
	return nil
}
