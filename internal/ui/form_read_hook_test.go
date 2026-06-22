package ui

// Issue #148: серверный обработчик формы ПриЧтенииНаСервере должен вызываться
// платформой на GET формы объекта ДО рендера HTML. Если обработчик бросает
// исключение (ВызватьИсключение) — платформа обязана отдать 403 и НЕ раскрывать
// данные записи. Это даёт конфигурациям RLS на чтение (row-level security),
// которого раньше не было: ПриОткрытии исполнялся только на клиенте, после
// того как сервер уже отдал форму со всеми полями.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/ivantit66/onebase/internal/metadata"
)

func executeFormEditGET(t *testing.T, s *Server, ent *metadata.Entity, id uuid.UUID) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("GET", "/ui/catalog/"+ent.Name+"/"+id.String(), nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("kind", "catalog")
	rctx.URLParams.Add("entity", ent.Name)
	rctx.URLParams.Add("id", id.String())
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()
	s.formEdit(rec, req)
	return rec
}

func insertContragent(t *testing.T, s *Server, ent *metadata.Entity, name string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	if err := s.store.Upsert(context.Background(), ent.Name, id,
		map[string]any{"Наименование": name}, ent); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestFormEdit_OnReadAtServerDeniesRead(t *testing.T) {
	srv, ent := setupManagedEventsServer(t, `
Процедура ПроверитьДоступ()
	ВызватьИсключение("Нет доступа к чужому документу");
КонецПроцедуры
`, map[metadata.FormEventType]string{
		metadata.FormEventType("ПриЧтенииНаСервере"): "ПроверитьДоступ",
	}, []*metadata.FormElement{
		{Kind: metadata.FormElementField, Name: "Наименование", DataPath: "Объект.Наименование"},
	})

	id := insertContragent(t, srv, ent, "СЕКРЕТНЫЙ-КОНТРАГЕНТ")
	rec := executeFormEditGET(t, srv, ent, id)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("ожидался 403 (ПриЧтенииНаСервере бросил исключение), получен %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "СЕКРЕТНЫЙ-КОНТРАГЕНТ") {
		t.Errorf("при отказе ПриЧтенииНаСервере форма не должна раскрывать данные записи")
	}
}

// Замечание #13: RLS-хук ПриЧтенииНаСервере, объявленный в обычной (autogen)
// форме объекта, тоже обязан выполняться. Раньше он искался лишь среди managed-
// форм и для autogen молча игнорировался (footgun для RLS на чтение).
func TestFormEdit_OnReadAtServerDeniesRead_AutogenForm(t *testing.T) {
	srv, ent := setupManagedEventsServer(t, `
Процедура ПроверитьДоступ()
	ВызватьИсключение("Нет доступа к чужому документу");
КонецПроцедуры
`, map[metadata.FormEventType]string{
		metadata.FormEventType("ПриЧтенииНаСервере"): "ПроверитьДоступ",
	}, []*metadata.FormElement{
		{Kind: metadata.FormElementField, Name: "Наименование", DataPath: "Объект.Наименование"},
	})

	// Делаем форму AUTOGEN (а не managed): pickManagedForm её бы не нашёл, и до
	// фикса хук не запустился бы. pickObjectFormWithReadHook обязан её подхватить.
	if len(ent.Forms) != 1 {
		t.Fatalf("ожидалась 1 форма у сущности, получено %d", len(ent.Forms))
	}
	ent.Forms[0].LayoutKind = metadata.FormLayoutAutogen
	if ent.Forms[0].IsManaged() {
		t.Fatal("форма должна стать autogen для этого теста")
	}

	id := insertContragent(t, srv, ent, "СЕКРЕТНЫЙ-АВТОГЕН")
	rec := executeFormEditGET(t, srv, ent, id)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("ожидался 403 (RLS-хук в autogen-форме бросил исключение), получен %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "СЕКРЕТНЫЙ-АВТОГЕН") {
		t.Errorf("при отказе RLS-хука autogen-форма не должна раскрывать данные записи")
	}
}

// Замечание #12: при ОБЪЯВЛЕННОМ обработчике ПриЧтенииНаСервере и ошибке загрузки
// объекта runFormReadHook обязан вернуть ошибку (fail-closed), а не nil — иначе
// форма отрисовалась бы, ни разу не выполнив RLS-хук (обход контроля доступа).
func TestRunFormReadHook_FailClosedOnLoadError(t *testing.T) {
	srv, ent := setupManagedEventsServer(t, `
Процедура ПроверитьДоступ()
КонецПроцедуры
`, map[metadata.FormEventType]string{
		metadata.FormEventType("ПриЧтенииНаСервере"): "ПроверитьДоступ",
	}, []*metadata.FormElement{
		{Kind: metadata.FormElementField, Name: "Наименование", DataPath: "Объект.Наименование"},
	})

	id := insertContragent(t, srv, ent, "ОБЫЧНЫЙ")

	// Добавляем сущности табличную часть ПОСЛЕ миграции — её таблицы в БД нет,
	// поэтому loadRuntimeObject упадёт на GetTablePartRows ещё ДО запуска хука.
	ent.TableParts = []metadata.TablePart{{
		Name:   "Строки",
		Fields: []metadata.Field{{Name: "Сумма", Type: metadata.FieldTypeNumber}},
	}}

	form := ent.Forms[0]
	err := srv.runFormReadHook(context.Background(), ent, form, id)
	if err == nil {
		t.Fatal("fail-open: при объявленном хуке и ошибке загрузки объекта ожидалась НЕ-nil ошибка (доступ запрещён)")
	}
}

func TestFormEdit_OnReadAtServerAllowsRead(t *testing.T) {
	srv, ent := setupManagedEventsServer(t, `
Процедура ПроверитьДоступ()
КонецПроцедуры
`, map[metadata.FormEventType]string{
		metadata.FormEventType("ПриЧтенииНаСервере"): "ПроверитьДоступ",
	}, []*metadata.FormElement{
		{Kind: metadata.FormElementField, Name: "Наименование", DataPath: "Объект.Наименование"},
	})

	id := insertContragent(t, srv, ent, "ОБЫЧНЫЙ-КОНТРАГЕНТ")
	rec := executeFormEditGET(t, srv, ent, id)

	if rec.Code != http.StatusOK {
		body := rec.Body.String()
		if len(body) > 300 {
			body = body[:300]
		}
		t.Fatalf("ожидался 200, получен %d; body=%s", rec.Code, body)
	}
	if !strings.Contains(rec.Body.String(), "ОБЫЧНЫЙ-КОНТРАГЕНТ") {
		t.Errorf("форма должна показывать данные, когда ПриЧтенииНаСервере разрешает чтение")
	}
}
