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

// Package taskpricing contains task pricing contracts and pure previews backed
// by the shared billingexpr core. It is not a price store, independent engine,
// HTTP API, or settlement implementation. Nothing in this package activates a
// new pricing profile for existing channels/tasks.
//
// Wire shapes and source precedence reference New API (AGPL-3.0), commit
// a2defd2a8256abe039e5295bd583aeb2d02f111f:
// model/model_pricing_config.go, setting/billing_setting/tiered_billing.go,
// pkg/jsplugin/registry.go and plugins/tasks/{hailuo,kling,doubao}/plugin.js.
// MAX additions are strict declared facts, final-required presence, separate
// contract/binding guards and explicit legacy profile selection. No upstream
// financial implementation is copied. See testdata/compat-v1.json for provenance
// and synthetic prices; these are not provider tariffs or production defaults.
package taskpricing
