/*
Copyright (C) 2023-2026 MAX-API-Next

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program.  If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact https://github.com/MAX-API-Next/MAX-API/issues
*/

package taskplugin

import (
	"errors"
	"sort"
	"strings"
	"sync/atomic"

	"github.com/MAX-API-Next/MAX-API/pkg/taskusage"
	"github.com/MAX-API-Next/MAX-API/types"
)

var ErrBindingRejected = errors.New("task plugin binding unavailable or unauthorized")

// Actor comes from the authenticated host context, not plugin/client metadata.
type Actor struct {
	UserID, TokenID, ChannelID int
	RequestID                  string
}

type hostState struct {
	generation uint64
	active     map[string]*Package
	history    map[string]*Package
	revoked    map[string]bool
	routes     Snapshot
}

// Host atomically publishes a complete route/package generation. Historical
// packages are retained so an upgrade cannot reinterpret an in-flight task.
type Host struct{ state atomic.Pointer[hostState] }

const MaxRetainedPackageBytes = 64 << 20

func NewHost() *Host {
	host := &Host{}
	host.state.Store(&hostState{active: map[string]*Package{}, history: map[string]*Package{}, revoked: map[string]bool{}, routes: NewRegistry().Snapshot()})
	return host
}

func packageIdentity(key, version string) string { return key + "@" + version }

// Install explicitly activates a batch all-or-nothing, including operator-chosen
// rollbacks. Use Retain to restore history without changing the active version.
func (host *Host) Install(packages ...*Package) (uint64, error) {
	return host.publishPackages(packages, true)
}

// Retain restores immutable historical packages without activating their routes
// or changing the active version. Several versions of one key may be retained
// together. Digest conflicts, revocations and the shared retention limit apply.
func (host *Host) Retain(packages ...*Package) (uint64, error) {
	return host.publishPackages(packages, false)
}

func (host *Host) publishPackages(packages []*Package, activate bool) (uint64, error) {
	if host == nil || host.state.Load() == nil || len(packages) == 0 {
		return 0, errors.New("task plugin host or package batch is empty")
	}
	seen := map[string]bool{}
	for _, p := range packages {
		if p == nil || p.digest == "" {
			return 0, errors.New("invalid or duplicate task plugin package")
		}
		identity := packageIdentity(p.manifest.Key, p.manifest.Version)
		if activate {
			identity = p.manifest.Key
		}
		if seen[identity] {
			return 0, errors.New("invalid or duplicate task plugin package")
		}
		seen[identity] = true
	}
	for {
		current := host.state.Load()
		next := cloneHostState(current)
		for _, p := range packages {
			id := packageIdentity(p.manifest.Key, p.manifest.Version)
			if old := current.history[id]; old != nil && old.digest != p.digest {
				return 0, errors.New("task plugin key/version source changed")
			}
			if current.revoked[id] {
				return 0, errors.New("task plugin package is revoked")
			}
			next.history[id] = p
			if activate {
				next.active[p.manifest.Key] = p
			}
		}
		total := 0
		for _, p := range next.history {
			total += p.size
		}
		if total > MaxRetainedPackageBytes {
			return 0, errors.New("task plugin retained package limit reached; historical packages were preserved")
		}
		if activate {
			registry := NewRegistry()
			keys := make([]string, 0, len(next.active))
			for key := range next.active {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			for _, key := range keys {
				if _, err := registry.Publish(next.active[key].manifest); err != nil {
					return 0, errors.New("task plugin batch has conflicting routes")
				}
			}
			next.routes = registry.Snapshot()
		}
		if host.state.CompareAndSwap(current, next) {
			return next.generation, nil
		}
	}
}

// Revoke disables both new bindings and old sessions for one immutable version.
// A process restart must reapply host-owned revocations before serving traffic.
func (host *Host) Revoke(key, version string) error {
	if host == nil || host.state.Load() == nil {
		return ErrBindingRejected
	}
	id := packageIdentity(key, version)
	for {
		current := host.state.Load()
		if current.history[id] == nil {
			return ErrBindingRejected
		}
		if current.revoked[id] {
			return nil
		}
		next := cloneHostState(current)
		next.revoked[id] = true
		if host.state.CompareAndSwap(current, next) {
			return nil
		}
	}
}

func cloneHostState(current *hostState) *hostState {
	next := &hostState{generation: current.generation + 1, active: map[string]*Package{}, history: map[string]*Package{}, revoked: map[string]bool{}, routes: current.routes}
	for k, v := range current.active {
		next.active[k] = v
	}
	for k, v := range current.history {
		next.history[k] = v
	}
	for k, v := range current.revoked {
		next.revoked[k] = v
	}
	return next
}

// Bind selects a route and freezes its package and host-owned usage contract.
func (host *Host) Bind(actor Actor, method, path, model string, contract types.TaskUsageContract) (types.TaskPluginBinding, error) {
	return host.BindMapped(actor, method, path, model, model, contract)
}

// BindMapped preserves the client's model alias while resolving the declared
// upstream model. Neither identity may be replaced during later polling.
func (host *Host) BindMapped(actor Actor, method, path, model, upstreamModel string, contract types.TaskUsageContract) (types.TaskPluginBinding, error) {
	if host == nil || host.state.Load() == nil || !validActor(actor) {
		return types.TaskPluginBinding{}, ErrBindingRejected
	}
	state := host.state.Load()
	match, ok := state.routes.MatchRoute(method, path, upstreamModel)
	if !ok {
		return types.TaskPluginBinding{}, ErrBindingRejected
	}
	p := state.active[match.PluginKey]
	if state.revoked[packageIdentity(p.manifest.Key, p.manifest.Version)] || model == "" || model != strings.TrimSpace(model) || !declaresModel(p.manifest, upstreamModel) {
		return types.TaskPluginBinding{}, ErrBindingRejected
	}
	digest, err := taskusage.ContractDigest(contract)
	if err != nil {
		return types.TaskPluginBinding{}, ErrBindingRejected
	}
	if _, err := taskusage.ResolveContract(contract.SourceID, contract.SchemaVersion, digest); err != nil {
		return types.TaskPluginBinding{}, ErrBindingRejected
	}
	return types.TaskPluginBinding{PluginKey: p.manifest.Key, Version: p.manifest.Version, PackageDigest: p.digest, SourceID: contract.SourceID, SchemaVersion: contract.SchemaVersion, ContractDigest: digest, Model: model, UpstreamModel: upstreamModel, UserID: actor.UserID, TokenID: actor.TokenID, ChannelID: actor.ChannelID, RequestID: actor.RequestID}, nil
}

// Resolve uses persisted identity, never the currently active version. Missing
// history or revoked code fails closed; it never retries a provider submission.
func (host *Host) Resolve(binding types.TaskPluginBinding, actor Actor) (*Session, error) {
	if host == nil || host.state.Load() == nil || !validActor(actor) || binding.UserID != actor.UserID || binding.TokenID != actor.TokenID || binding.ChannelID != actor.ChannelID || binding.RequestID != actor.RequestID {
		return nil, ErrBindingRejected
	}
	state := host.state.Load()
	id := packageIdentity(binding.PluginKey, binding.Version)
	p := state.history[id]
	if p == nil || state.revoked[id] || binding.PackageDigest != p.digest || binding.Model == "" || binding.Model != strings.TrimSpace(binding.Model) || !declaresModel(p.manifest, binding.UpstreamModel) {
		return nil, ErrBindingRejected
	}
	contract, err := taskusage.ResolveContract(binding.SourceID, binding.SchemaVersion, binding.ContractDigest)
	if err != nil {
		return nil, ErrBindingRejected
	}
	return &Session{host: host, binding: binding, actor: actor, contract: contract, plugin: p}, nil
}

func validActor(actor Actor) bool {
	return actor.UserID > 0 && actor.TokenID > 0 && actor.ChannelID > 0 && actor.RequestID != "" && actor.RequestID == strings.TrimSpace(actor.RequestID)
}

// Session holds private, immutable host context; it is not a JavaScript VM.
type Session struct {
	host     *Host
	binding  types.TaskPluginBinding
	actor    Actor
	contract types.TaskUsageContract
	plugin   *Package
}

func (session *Session) validate() error {
	if session == nil {
		return ErrBindingRejected
	}
	_, err := session.host.Resolve(session.binding, session.actor)
	return err
}

func (session *Session) Binding() types.TaskPluginBinding { return session.binding }
func (session *Session) UsageContract() types.TaskUsageContract {
	return cloneUsageContract(session.contract)
}

// UsageEnvelope accepts facts only. Funding and task state stay in MAX-API's
// existing settlement and polling layers, including explicit-zero semantics.
func (session *Session) UsageEnvelope(usage *types.TaskUsage) (*types.TaskUsageEnvelope, error) {
	if err := session.validate(); err != nil {
		return nil, err
	}
	return taskusage.BuildEnvelope(types.TaskUsageProducerKindTaskPlugin, session.contract, types.TaskUsageSourceProviderResponse, usage)
}
