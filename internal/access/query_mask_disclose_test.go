package access

import (
	"testing"

	"github.com/ivantit66/onebase/internal/auth"
	"github.com/ivantit66/onebase/internal/metadata"
	"github.com/ivantit66/onebase/internal/query"
)

func discloseTestEntity() *metadata.Entity {
	return &metadata.Entity{
		Name: "Заявка", Kind: metadata.KindDocument,
		Fields: []metadata.Field{
			{Name: "Номер", Type: metadata.FieldTypeString},
			{Name: "Телефон", Type: metadata.FieldTypeString, PII: true},
		},
	}
}

func discloseUser(ops ...string) *auth.User {
	return &auth.User{ID: "u1", Login: "оператор", Roles: []*auth.Role{{
		Name:        "Оператор",
		Permissions: auth.Permission{Documents: map[string][]string{"Заявка": ops}},
	}}}
}

// Отбор по защищённому полю запрещён тому, кто не может увидеть значение.
func TestQueryMaskPlan_FilterDeniedWithoutDisclose(t *testing.T) {
	ent := discloseTestEntity()
	res, err := query.Compile(`ВЫБРАТЬ Номер ИЗ Документ.Заявка ГДЕ Телефон = &Т`,
		query.CompileOpts{Entities: []*metadata.Entity{ent}})
	if err != nil {
		t.Fatal(err)
	}
	plan := QueryMaskPlanFor(discloseUser("read"), res, func(kind, name string) *metadata.Entity { return ent })
	if plan.Denied == "" {
		t.Fatal("отбор по ПДн без права disclose должен запрещаться")
	}
}

// А тому, кто вправе раскрыть значение кнопкой, отбор по нему разрешён:
// подбор не даёт ему ничего нового, а без отбора он не найдёт заявку по
// телефону, который клиент только что назвал.
func TestQueryMaskPlan_FilterAllowedWithDisclose(t *testing.T) {
	ent := discloseTestEntity()
	res, err := query.Compile(`ВЫБРАТЬ Номер ИЗ Документ.Заявка ГДЕ Телефон = &Т`,
		query.CompileOpts{Entities: []*metadata.Entity{ent}})
	if err != nil {
		t.Fatal(err)
	}
	plan := QueryMaskPlanFor(discloseUser("read", "disclose"), res, func(kind, name string) *metadata.Entity { return ent })
	if plan.Denied != "" {
		t.Fatalf("отбор запрещён при наличии disclose: %s", plan.Denied)
	}
}

// Право разрешает ИСКАТЬ, но не ЧИТАТЬ: вывод самого поля остаётся под маской.
func TestQueryMaskPlan_DiscloseStillMasksOutput(t *testing.T) {
	ent := discloseTestEntity()
	res, err := query.Compile(`ВЫБРАТЬ Номер, Телефон ИЗ Документ.Заявка`,
		query.CompileOpts{Entities: []*metadata.Entity{ent}})
	if err != nil {
		t.Fatal(err)
	}
	plan := QueryMaskPlanFor(discloseUser("read", "disclose"), res, func(kind, name string) *metadata.Entity { return ent })
	rows := []map[string]any{{"номер": "Пл1", "телефон": "+79990000000"}}
	if _, err := plan.ApplyTracked(rows); err != nil {
		t.Fatal(err)
	}
	if rows[0]["телефон"] == "+79990000000" {
		t.Fatal("значение ПДн отдано столбцом без маски")
	}
}
