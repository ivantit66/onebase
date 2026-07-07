package configdb

// White-box тест монотонных created_at версий: системный таймер (особенно на
// Windows) может вернуть одинаковый time.Now() для подряд идущих версий, и
// раньше порядок решал тай-брейк по случайному UUID — история конфигурации
// становилась недетерминированной (флак TestRepoVersions_SaveDiffRollback).

import "testing"

func TestNextVersionTimeStrictlyIncreasing(t *testing.T) {
	prev := nextVersionTime()
	for i := 0; i < 10_000; i++ {
		cur := nextVersionTime()
		if !cur.After(prev) {
			t.Fatalf("итерация %d: %v не позже %v", i, cur, prev)
		}
		prev = cur
	}
}
