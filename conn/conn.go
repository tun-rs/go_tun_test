/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2017-2025 WireGuard LLC. All Rights Reserved.
 */

// Package conn implements WireGuard's network connections.
package conn

const (
	IdealBatchSize = 128 // maximum number of packets handled per read and write
)
