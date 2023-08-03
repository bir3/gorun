// Copyright 2022 Bergur Ragnarsson
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cache

import (
	"testing"
	"time"
)

func TestAbs(t *testing.T) {
	t1 := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2000, 1, 1, 1, 0, 0, 0, time.UTC)

	if timeDiff(t1, t2) != time.Hour {
		t.Error("failed")
	}
	if timeDiff(t2, t1) != time.Hour {
		t.Error("failed")
	}

}
