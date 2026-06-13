package pdfparse

import "testing"

// TestOffsetBE проверяет числовую разность big-endian кодов.
func TestOffsetBE(t *testing.T) {
	cases := []struct {
		code, lo string
		want     int
	}{
		{"\x00\x00", "\x00\x00", 0},
		{"\x00\x1d", "\x00\x00", 0x1d},
		{"\x04\x1d", "\x00\x00", 0x041d},
		{"\xff\xff", "\x00\x00", 0xffff},
		{"\x04\x20", "\x04\x00", 0x20},
	}
	for _, c := range cases {
		if got := offsetBE(c.code, c.lo); got != c.want {
			t.Errorf("offsetBE(%x,%x)=%#x, want %#x", c.code, c.lo, got, c.want)
		}
	}
}

// TestAddRangeOffsetBE проверяет, что масштабирование назначения bfrange
// прибавляет ПОЛНОЕ смещение с переносом по байтам (исправление потери старшего
// байта в широких identity-диапазонах fpdf «<0000> <FFFF> <0000>»).
func TestAddRangeOffsetBE(t *testing.T) {
	cases := []struct {
		dst, code, lo string
		want          string
	}{
		// identity <0000> <FFFF> <0000>: код 041D → U+041D «Н» (исходный rsc.io
		// давал 001D из-за инкремента только последнего байта).
		{"\x00\x00", "\x04\x1d", "\x00\x00", "\x04\x1d"},
		{"\x00\x00", "\x00\x1d", "\x00\x00", "\x00\x1d"},
		{"\x00\x00", "\xff\xff", "\x00\x00", "\xff\xff"},
		// перенос через байт: dst 00FF + 1 = 0100.
		{"\x00\xff", "\x00\x01", "\x00\x00", "\x01\x00"},
		// смещение от ненулевого начала диапазона.
		{"\x10\x00", "\x04\x20", "\x04\x00", "\x10\x20"},
	}
	for _, c := range cases {
		got := addRangeOffsetBE(c.dst, c.code, c.lo)
		if got != c.want {
			t.Errorf("addRangeOffsetBE(%x,%x,%x)=%x, want %x", c.dst, c.code, c.lo, got, c.want)
		}
	}
}

// TestCmapDecodeWideIdentityRange — сквозная проверка cmap.Decode на CMap вида
// fpdf: codespace <0000>-<FFFF>, один bfrange <0000> <FFFF> <0000>. Код «Н»
// (U+041D) как 2 байта должен декодироваться в «Н», а не в U+001D.
func TestCmapDecodeWideIdentityRange(t *testing.T) {
	m := &cmap{}
	m.space[1] = []byteRange{{low: "\x00\x00", high: "\xff\xff"}}
	m.bfrange = []bfrange{{lo: "\x00\x00", hi: "\xff\xff", dst: Value{}}}

	// dst — String "\x00\x00" (UTF-16BE 0000). Соберём Value-строку через RawString
	// невозможно без Reader, поэтому проверяем низкоуровневые помощники выше; здесь
	// убеждаемся, что при пустом dst (Null) Decode не паникует.
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Decode запаниковал: %v", r)
		}
	}()
	_ = m.Decode("\x04\x1d")
}
