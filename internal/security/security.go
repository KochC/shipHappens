// Package security computes the effective network policy for a job from the
// workflow security policy and the job's own settings. The goal is
// supply-chain-safe defaults: steps run offline unless they explicitly need
// network, and when they do, egress is scoped to an allow-list.
package security

import "github.com/chris/shiphappens/internal/compiler"

// NetMode is the resolved networking decision for a job.
type NetMode int

const (
	// NetDefault: engine default networking (full egress). Used when no policy
	// restricts the job.
	NetDefault NetMode = iota
	// NetNone: no network at all (--network none). The safe default under an
	// offline-by-default policy.
	NetNone
	// NetAllow: network is on but egress should be restricted to Allow hosts.
	NetAllow
)

// Decision is the resolved policy for a job.
type Decision struct {
	Mode  NetMode
	Allow []string // effective allow-list when Mode == NetAllow
}

// Resolve computes the network decision for a job given the workflow policy.
//
// Precedence:
//  1. An explicit job Network=false  → NetNone (hard isolation).
//  2. A job Allow list               → NetAllow (opt-in with allow-list).
//  3. A job Network=true             → NetDefault (explicit full network).
//  4. OfflineByDefault policy        → NetNone (offline unless opted in).
//  5. A policy DefaultAllow list     → NetAllow with that list.
//  6. Otherwise                      → NetDefault.
func Resolve(policy *compiler.SecurityPolicy, job *compiler.JobPlan) Decision {
	if job.Network != nil && !*job.Network {
		return Decision{Mode: NetNone}
	}
	if len(job.Allow) > 0 {
		return Decision{Mode: NetAllow, Allow: job.Allow}
	}
	if job.Network != nil && *job.Network {
		return Decision{Mode: NetDefault}
	}
	if policy != nil && policy.OfflineByDefault {
		return Decision{Mode: NetNone}
	}
	if policy != nil && len(policy.DefaultAllow) > 0 {
		return Decision{Mode: NetAllow, Allow: policy.DefaultAllow}
	}
	return Decision{Mode: NetDefault}
}
