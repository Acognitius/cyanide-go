//go:build !windows

/* SPDX-License-Identifier: MIT
 *
  * Copyright (C) 2017-2023 WireGuard LLC. All Rights Reserved.
  * Copyright (C) 2023 Synthesis Labs. All Rights Reserved.
 */

package conn

func NewDefaultBind() Bind { return NewStdNetBind() }
