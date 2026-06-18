package interpreter_test

import (
	"strings"
	"testing"

	"github.com/ivantit66/onebase/internal/dsl/ast"
	"github.com/ivantit66/onebase/internal/dsl/interpreter"
	"github.com/ivantit66/onebase/internal/dsl/lexer"
	"github.com/ivantit66/onebase/internal/dsl/parser"
	"github.com/ivantit66/onebase/internal/metadata"
	"github.com/ivantit66/onebase/internal/runtime"
)

func runProc(t *testing.T, src string, obj *runtime.Object) error {
	t.Helper()
	l := lexer.New(src, "test.os")
	p := parser.New(l)
	prog, err := p.ParseProgram()
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(prog.Procedures) == 0 {
		t.Fatal("no procedures")
	}
	interp := interpreter.New()
	return interp.Run(prog.Procedures[0], obj)
}

func TestInterpreter_VarDeclCommaList(t *testing.T) {
	// Несколько переменных в одном Перем через запятую должны объявляться все.
	src := `Процедура Выполнить()
  Перем а, б, в;
  а = 10;
  б = 20;
  в = а + б;
  this.Результат = в;
КонецПроцедуры`

	obj := runtime.NewObject("Test", metadata.KindDocument)
	if err := runProc(t, src, obj); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !numEq(obj.Get("Результат"), 30) {
		t.Fatalf("expected 30, got %v", obj.Get("Результат"))
	}
}

func TestInterpreter_ErrorOnEmptyNumber(t *testing.T) {
	src := `Procedure OnWrite()
  If this.Number = "" Then
    Error("Number is required");
  EndIf;
EndProcedure`

	obj := runtime.NewObject("Invoice", metadata.KindDocument)
	obj.Set("Number", "")

	err := runProc(t, src, obj)
	if err == nil {
		t.Fatal("expected error for empty Number")
	}
	dslErr, ok := err.(*interpreter.DSLError)
	if !ok {
		t.Fatalf("want DSLError, got %T: %v", err, err)
	}
	if dslErr.Msg != "Number is required" {
		t.Fatalf("wrong message: %q", dslErr.Msg)
	}
}

func TestInterpreter_NoErrorWithNumber(t *testing.T) {
	src := `Procedure OnWrite()
  If this.Number = "" Then
    Error("Number is required");
  EndIf;
EndProcedure`

	obj := runtime.NewObject("Invoice", metadata.KindDocument)
	obj.Set("Number", "INV-001")

	if err := runProc(t, src, obj); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestInterpreter_Assign(t *testing.T) {
	src := `Procedure SetField()
  this.Status = "active";
EndProcedure`

	obj := runtime.NewObject("Invoice", metadata.KindDocument)
	if err := runProc(t, src, obj); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if obj.Get("Status") != "active" {
		t.Fatalf("expected Status=active, got %v", obj.Get("Status"))
	}
}

func TestInterpreter_UnknownFunction(t *testing.T) {
	src := `Procedure Bad()
  DoSomething();
EndProcedure`
	obj := runtime.NewObject("X", metadata.KindDocument)
	err := runProc(t, src, obj)
	if err == nil {
		t.Fatal("expected error for unknown function")
	}
}

// Регрессия для замечания #13: несколько процедур в одном файле
// (.proc.os и т.п.) должны вызывать друг друга. Реализовано через
// Interpreter.LookupSiblingProc — резолвер по файлу.
func TestInterpreter_SiblingProcResolution(t *testing.T) {
	src := `Процедура Помощник()
  ЭтотОбъект.Метка = "ок";
КонецПроцедуры

Процедура Главная()
  Помощник();
КонецПроцедуры`

	l := lexer.New(src, "test.proc.os")
	p := parser.New(l)
	prog, err := p.ParseProgram()
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(prog.Procedures) != 2 {
		t.Fatalf("ожидалось 2 процедуры, получили %d", len(prog.Procedures))
	}
	var main, helper *ast.ProcedureDecl
	for _, pr := range prog.Procedures {
		if strings.EqualFold(pr.Name.Literal, "Главная") {
			main = pr
		} else if strings.EqualFold(pr.Name.Literal, "Помощник") {
			helper = pr
		}
	}
	if main == nil || helper == nil {
		t.Fatal("процедуры не нашлись")
	}

	interp := interpreter.New()
	interp.LookupSiblingProc = func(file, name string) *ast.ProcedureDecl {
		if file != "test.proc.os" {
			return nil
		}
		if strings.EqualFold(name, "Помощник") {
			return helper
		}
		return nil
	}
	obj := runtime.NewObject("X", metadata.KindDocument)
	if err := interp.Run(main, obj); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if obj.Get("Метка") != "ок" {
		t.Errorf("expected Метка=ок, got %v", obj.Get("Метка"))
	}
}

// Module.Proc() namespaced calls.
func TestInterpreter_NamespacedModuleProc(t *testing.T) {
	helperSrc := `Функция Удвоить(Х)
  Возврат Х * 2;
КонецФункции`
	l := lexer.New(helperSrc, "утилиты.module.os")
	p := parser.New(l)
	helperProg, err := p.ParseProgram()
	if err != nil {
		t.Fatal(err)
	}
	helper := helperProg.Procedures[0]

	mainSrc := `Процедура Главная()
  ЭтотОбъект.Результат = Утилиты.Удвоить(21);
КонецПроцедуры`
	l2 := lexer.New(mainSrc, "test.proc.os")
	p2 := parser.New(l2)
	mainProg, _ := p2.ParseProgram()
	main := mainProg.Procedures[0]

	interp := interpreter.New()
	interp.LookupModuleProc = func(module, name string) *ast.ProcedureDecl {
		if strings.EqualFold(module, "Утилиты") && strings.EqualFold(name, "Удвоить") {
			return helper
		}
		return nil
	}
	obj := runtime.NewObject("X", metadata.KindDocument)
	if err := interp.Run(main, obj); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !numEq(obj.Get("Результат"), 42) {
		t.Errorf("expected 42, got %v", obj.Get("Результат"))
	}
}

// Без правильного file-контекста sibling lookup не должен пускать.
func TestInterpreter_SiblingProcScopedByFile(t *testing.T) {
	src := `Процедура Главная()
  ЧужойПомощник();
КонецПроцедуры`

	l := lexer.New(src, "main.proc.os")
	p := parser.New(l)
	prog, err := p.ParseProgram()
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	main := prog.Procedures[0]

	// helper определён в другом файле
	helperSrc := `Процедура ЧужойПомощник()
  ЭтотОбъект.Метка = "не должно";
КонецПроцедуры`
	l2 := lexer.New(helperSrc, "other.proc.os")
	p2 := parser.New(l2)
	helperProg, _ := p2.ParseProgram()
	helper := helperProg.Procedures[0]

	interp := interpreter.New()
	interp.LookupSiblingProc = func(file, name string) *ast.ProcedureDecl {
		// возвращаем только если совпал файл
		if file == helper.Name.File && strings.EqualFold(name, "ЧужойПомощник") {
			return helper
		}
		return nil
	}
	obj := runtime.NewObject("X", metadata.KindDocument)
	if err := interp.Run(main, obj); err == nil {
		t.Fatal("ожидалась ошибка: чужой файл не должен резолвиться")
	}
}
