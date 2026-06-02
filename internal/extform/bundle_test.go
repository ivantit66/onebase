package extform

import (
	"strings"
	"testing"
)

const bareForm = `name: Накладная
document: РеализацияТоваров
title: Накладная
`

const bundleForm = `manifest:
  kind: printform
  name: Накладная
  document: РеализацияТоваров
  author: Иван
  version: 2.1.0
  min_platform: 0.1.0
form:
  name: Накладная
  document: РеализацияТоваров
  title: Накладная из бандла
`

func TestParseUpload_Bare(t *testing.T) {
	p, err := ParseUpload([]byte(bareForm))
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "Накладная" || p.Document != "РеализацияТоваров" {
		t.Errorf("имя/документ из формы: %+v", p)
	}
	if !strings.Contains(string(p.Content), "title: Накладная") {
		t.Error("Content должен быть голым YAML формы")
	}
}

func TestParseUpload_Bundle(t *testing.T) {
	p, err := ParseUpload([]byte(bundleForm))
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "Накладная" || p.Document != "РеализацияТоваров" {
		t.Errorf("имя/документ: %+v", p)
	}
	if p.Author != "Иван" || p.Version != "2.1.0" || p.MinPlatform != "0.1.0" {
		t.Errorf("манифест не разобран: %+v", p)
	}
	// Content — это тело form, без manifest.
	if strings.Contains(string(p.Content), "manifest") {
		t.Error("Content не должен содержать manifest")
	}
	if !strings.Contains(string(p.Content), "Накладная из бандла") {
		t.Errorf("Content должен быть телом form: %s", p.Content)
	}
}

func TestParseUpload_MissingFields(t *testing.T) {
	if _, err := ParseUpload([]byte("title: без имени\n")); err == nil {
		t.Error("ожидалась ошибка при отсутствии name/document")
	}
}

func TestBuildBundle_RoundTrip(t *testing.T) {
	rec := &Record{
		Document: "РеализацияТоваров",
		Name:     "Накладная",
		Content:  []byte(bareForm),
		Author:   "Пётр",
		Version:  "1.2.3",
	}
	data, err := BuildBundle(rec, "0.5.0")
	if err != nil {
		t.Fatal(err)
	}
	// Бандл снова разбирается ParseUpload и даёт ту же форму.
	p, err := ParseUpload(data)
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "Накладная" || p.Document != "РеализацияТоваров" {
		t.Errorf("round-trip потерял имя/документ: %+v", p)
	}
	if p.Author != "Пётр" || p.Version != "1.2.3" || p.MinPlatform != "0.5.0" {
		t.Errorf("round-trip потерял манифест: %+v", p)
	}
}

func TestCheckMinPlatform(t *testing.T) {
	cases := []struct {
		min, cur string
		wantErr  bool
	}{
		{"", "0.1.0", false},          // не задано
		{"0.1.0", "0.2.0", false},     // current выше
		{"0.2.0", "0.2.0", false},     // равны
		{"0.3.0", "0.2.0", true},      // current ниже
		{"1.0", "0.9", true},          // разной длины
		{"abc", "0.2.0", false},       // не парсится → пропускаем
		{"0.2.0", "не-версия", false}, // не парсится → пропускаем
	}
	for _, c := range cases {
		err := CheckMinPlatform(c.min, c.cur)
		if (err != nil) != c.wantErr {
			t.Errorf("CheckMinPlatform(%q,%q): err=%v, wantErr=%v", c.min, c.cur, err, c.wantErr)
		}
	}
}
