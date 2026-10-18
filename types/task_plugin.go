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

package types

// TaskPluginBinding is host-issued private task metadata. It carries no
// credentials or pricing and must never be accepted from a client body.
// Persist it with the task before sending any billable upstream request.
type TaskPluginBinding struct {
	PluginKey      string `json:"plugin_key"`
	Version        string `json:"version"`
	PackageDigest  string `json:"package_digest"`
	SourceID       string `json:"source_id"`
	SchemaVersion  int    `json:"schema_version"`
	ContractDigest string `json:"contract_digest"`
	Model          string `json:"model"`
	UpstreamModel  string `json:"upstream_model"`
	UserID         int    `json:"user_id"`
	TokenID        int    `json:"token_id"`
	ChannelID      int    `json:"channel_id"`
	RequestID      string `json:"request_id"`
}
