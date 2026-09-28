package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/ivantit66/onebase/internal/auth"
	"github.com/ivantit66/onebase/internal/metadata"
	"github.com/ivantit66/onebase/internal/runtime"
	"github.com/ivantit66/onebase/internal/storage"
)

// Числовой конец глубокого источника на обезличенном адресном примере: дом
// связан с улицей КОДОМ, ссылки между ними нет вовсе. Проверяется публичным
// маршрутом подбора — значение кода читает сервер под правами пользователя, из
// браузера приходит только ссылка выбранной улицы.
type scalarSourceFixture struct {
	server  *Server
	houses  *metadata.Entity
	owner   *metadata.Entity
	streetA uuid.UUID // код 100, видна оператору
	streetB uuid.UUID // код 200, видна оператору
	streetC uuid.UUID // код 100, закрыта строковым доступом
	streetD uuid.UUID // код не заполнен
}

func newScalarSourceFixture(t *testing.T) scalarSourceFixture {
	t.Helper()
	ctx := context.Background()
	db, err := storage.ConnectSQLite(ctx, filepath.Join(t.TempDir(), "scalar-source.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	streets := &metadata.Entity{
		Name: "УлицыКлассификатора", Kind: metadata.KindCatalog,
		Fields: []metadata.Field{
			{Name: "Наименование", Type: metadata.FieldTypeString},
			{Name: "ИД", Type: metadata.FieldTypeNumber},
			{Name: "Аудитория", Type: metadata.FieldTypeString},
		},
	}
	houses := &metadata.Entity{
		Name: "ДомаКлассификатора", Kind: metadata.KindCatalog,
		Fields: []metadata.Field{
			{Name: "Наименование", Type: metadata.FieldTypeString},
			{Name: "ВладелецКод", Type: metadata.FieldTypeNumber},
		},
	}
	form := &metadata.FormModule{
		Name: "ФормаОбъекта", EntityName: "Заявка", Kind: "object", LayoutKind: metadata.FormLayoutManaged,
		Elements: []*metadata.FormElement{
			{ID: "street", Name: "ПолеУлица", Kind: metadata.FormElementField, DataPath: "Объект.Улица"},
			{
				ID: "house", Name: "ПолеДом", Kind: metadata.FormElementField,
				DataPath: "Объект.Дом", Choice: true,
				ChoiceFilter: []metadata.FormChoiceCondition{{
					Field: "ВладелецКод", Op: metadata.FormChoiceOpEqual, From: "Объект.Улица.ИД",
				}},
			},
		},
	}
	owner := &metadata.Entity{
		Name: "Заявка", Kind: metadata.KindDocument,
		Fields: []metadata.Field{
			{Name: "Улица", Type: metadata.FieldType("reference:" + streets.Name), RefEntity: streets.Name},
			{Name: "Дом", Type: metadata.FieldType("reference:" + houses.Name), RefEntity: houses.Name},
		},
		Forms: []*metadata.FormModule{form},
	}
	entities := []*metadata.Entity{streets, houses, owner}
	if err := db.Migrate(ctx, entities); err != nil {
		t.Fatal(err)
	}

	fixture := scalarSourceFixture{
		server: nil, houses: houses, owner: owner,
		streetA: uuid.MustParse("40000000-0000-0000-0000-000000000001"),
		streetB: uuid.MustParse("40000000-0000-0000-0000-000000000002"),
		streetC: uuid.MustParse("40000000-0000-0000-0000-000000000003"),
		streetD: uuid.MustParse("40000000-0000-0000-0000-000000000004"),
	}
	for _, row := range []struct {
		id       uuid.UUID
		name     string
		code     any
		audience string
	}{
		{fixture.streetA, "улица Первая", "100", "anna"},
		{fixture.streetB, "улица Вторая", "200", "anna"},
		{fixture.streetC, "улица Третья", "100", "bob"},
		{fixture.streetD, "улица Четвёртая", nil, "anna"},
	} {
		if err := db.Upsert(ctx, streets.Name, row.id, map[string]any{
			"Наименование": row.name, "ИД": row.code, "Аудитория": row.audience,
		}, streets); err != nil {
			t.Fatalf("seed street: %v", err)
		}
	}
	for index, row := range []struct {
		name string
		code string
	}{
		{"дом 1", "100"},
		{"дом 2", "200"},
		// Тот же код, записанный иначе: на SQLite число лежит текстом, и
		// сравнение обязано идти по каноническому виду, а не по строке.
		{"дом 3", "100.0"},
	} {
		id := uuid.MustParse(fmt.Sprintf("50000000-0000-0000-0000-%012d", index+1))
		if err := db.Upsert(ctx, houses.Name, id, map[string]any{
			"Наименование": row.name, "ВладелецКод": row.code,
		}, houses); err != nil {
			t.Fatalf("seed house: %v", err)
		}
	}

	reg := runtime.NewRegistry()
	reg.Load(runtime.LoadOptions{Entities: entities})
	fixture.server = &Server{reg: reg, store: db}
	fixture.server.entitySvc = fixture.server.newEntityService(nil)
	return fixture
}

func scalarSourceUser(policies auth.FieldPolicies) *auth.User {
	permission := auth.Permission{
		Catalogs:  map[string][]string{"УлицыКлассификатора": {"read"}, "ДомаКлассификатора": {"read"}},
		Documents: map[string][]string{"Заявка": {"read", "write"}},
		RowAccess: auth.RowAccess{Catalogs: map[string]auth.RowPolicies{
			"УлицыКлассификатора": {"read": {Field: "Аудитория", Op: "eq", Value: auth.RowValue{User: "login"}}},
		}},
	}
	if len(policies) > 0 {
		permission.FieldAccess = auth.FieldAccess{Catalogs: map[string]auth.FieldPolicies{"УлицыКлассификатора": policies}}
	}
	return &auth.User{Login: "anna", Roles: []*auth.Role{{Permissions: permission}}}
}

func (f scalarSourceFixture) houseLabels(t *testing.T, user *auth.User, street string) []string {
	t.Helper()
	sources, err := json.Marshal(map[string]string{"Объект.Улица.ИД": street})
	if err != nil {
		t.Fatal(err)
	}
	query := url.Values{
		"form_entity": {f.owner.Name}, "form": {"ФормаОбъекта"},
		"element": {"house"}, "sources": {string(sources)}, "limit": {"100"},
	}
	router := chi.NewRouter()
	f.server.Mount(router)
	request := httptest.NewRequest(http.MethodGet, "/ui/_ref-options/"+url.PathEscape(f.houses.Name)+"?"+query.Encode(), nil)
	request = request.WithContext(auth.ContextWithUser(request.Context(), user))
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	response := decodeChoiceHTTP(t, recorder)
	labels := make([]string, 0, len(response.Items))
	for _, item := range response.Items {
		labels = append(labels, fmt.Sprint(item["_label"]))
	}
	return labels
}

func TestRefOptionsScalarDeepSourceFiltersByCode(t *testing.T) {
	f := newScalarSourceFixture(t)
	user := scalarSourceUser(nil)

	t.Run("дома отбираются кодом выбранной улицы", func(t *testing.T) {
		labels := f.houseLabels(t, user, f.streetA.String())
		if len(labels) != 2 {
			t.Fatalf("ожидались дома с кодом 100, получено %v", labels)
		}
		for _, label := range labels {
			if label == "дом 2" {
				t.Fatalf("дом чужой улицы попал в подбор: %v", labels)
			}
		}
		if other := f.houseLabels(t, user, f.streetB.String()); len(other) != 1 || other[0] != "дом 2" {
			t.Fatalf("смена улицы не пересчитала отбор: %v", other)
		}
	})

	t.Run("закрытая строковым доступом улица не раскрывает свой код", func(t *testing.T) {
		if labels := f.houseLabels(t, user, f.streetC.String()); len(labels) != 0 {
			t.Fatalf("улица чужого оператора раскрыла код: %v", labels)
		}
	})

	t.Run("незаполненный код даёт пустую выдачу", func(t *testing.T) {
		if labels := f.houseLabels(t, user, f.streetD.String()); len(labels) != 0 {
			t.Fatalf("пустой код отобрал весь справочник: %v", labels)
		}
	})

	t.Run("несуществующая улица неотличима от закрытой", func(t *testing.T) {
		missing := uuid.MustParse("40000000-0000-0000-0000-000000000099")
		if labels := f.houseLabels(t, user, missing.String()); len(labels) != 0 {
			t.Fatalf("выдача по несуществующей улице: %v", labels)
		}
	})

	t.Run("код под полевой политикой в отборе не участвует", func(t *testing.T) {
		for _, strategy := range []string{"hide", "mask_all"} {
			masked := scalarSourceUser(auth.FieldPolicies{"ИД": auth.FieldPolicy{Read: strategy}})
			if labels := f.houseLabels(t, masked, f.streetA.String()); len(labels) != 0 {
				t.Fatalf("политика %q обойдена подбором: %v", strategy, labels)
			}
		}
	})

	t.Run("пустой источник не показывает весь справочник", func(t *testing.T) {
		if labels := f.houseLabels(t, user, ""); len(labels) != 0 {
			t.Fatalf("без выбранной улицы показан весь справочник: %v", labels)
		}
	})
}
