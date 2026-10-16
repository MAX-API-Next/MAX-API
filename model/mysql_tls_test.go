/*
Copyright (C) 2023-2026 MAX-API-Next

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program.  If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact https://github.com/MAX-API-Next/MAX-API/issues
*/

package model

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMySQLDSNWithPinnedCertificateLeavesDefaultUnchanged(t *testing.T) {
	t.Setenv("MYSQL_TLS_CERT_SHA256", "")
	dsn := "user:password@tcp(127.0.0.1:3306)/database?parseTime=true"
	got, err := mysqlDSNWithPinnedCertificate(dsn)
	require.NoError(t, err)
	require.Equal(t, dsn, got)
}

func TestMySQLDSNWithPinnedCertificateRejectsInvalidFingerprint(t *testing.T) {
	t.Setenv("MYSQL_TLS_CERT_SHA256", "not-a-fingerprint")
	_, err := mysqlDSNWithPinnedCertificate("user:password@tcp(127.0.0.1:3306)/database")
	require.EqualError(t, err, "MYSQL_TLS_CERT_SHA256 must be a 64-character SHA-256 fingerprint")
}

func TestMySQLDSNWithPinnedCertificateAddsReusableTLSConfig(t *testing.T) {
	fingerprint := strings.Repeat("a", 64)
	t.Setenv("MYSQL_TLS_CERT_SHA256", fingerprint)
	dsn := "user:password@tcp(127.0.0.1:3306)/database?parseTime=true"
	got, err := mysqlDSNWithPinnedCertificate(dsn)
	require.NoError(t, err)
	require.Contains(t, got, "parseTime=true")
	require.Contains(t, got, "tls=max-api-mysql-pin-"+fingerprint)

	reused, err := mysqlDSNWithPinnedCertificate(dsn)
	require.NoError(t, err)
	require.Equal(t, got, reused)
}

func TestMySQLDSNWithPinnedCertificateReplacesUnpinnedTLSOption(t *testing.T) {
	t.Setenv("MYSQL_TLS_CERT_SHA256", strings.Repeat("b", 64))
	got, err := mysqlDSNWithPinnedCertificate("user:password@tcp(127.0.0.1:3306)/database?tls=skip-verify&parseTime=true")
	require.NoError(t, err)
	require.Contains(t, got, "tls=max-api-mysql-pin-"+strings.Repeat("b", 64))
	require.NotContains(t, got, "tls=skip-verify")
}

func TestMySQLDSNWithPinnedCertificatePreservesDriverDSNParsing(t *testing.T) {
	fingerprint := strings.Repeat("c", 64)
	t.Setenv("MYSQL_TLS_CERT_SHA256", fingerprint)
	dsn := "user:pa?ss@tcp(127.0.0.1:3306)/database?parseTime=true&loc=Asia%2FShanghai"
	got, err := mysqlDSNWithPinnedCertificate(dsn)
	require.NoError(t, err)
	require.Contains(t, got, "tls=max-api-mysql-pin-"+fingerprint)
	require.Contains(t, got, "parseTime=true")
	require.Contains(t, got, "loc=Asia%2FShanghai")
}
