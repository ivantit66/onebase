package interpreter

import (
	"errors"
	"time"

	"github.com/ivantit66/onebase/internal/dsl/ast"
)

var (
	errSandboxTimeout = errors.New("превышено максимальное время выполнения (песочница)")
	errSandboxIters   = errors.New("превышен лимит итераций цикла (песочница)")
)

// SandboxProfile описывает, что разрешено одному запуску DSL. Нулевое значение =
// «всё разрешено» = поведение по умолчанию (без регрессии).
type SandboxProfile struct {
	AllowNet     bool          // сеть: HTTP-клиент и email
	AllowFile    bool          // файловые builtins
	AllowExec    bool          // выполнение команд ОС (ВыполнитьКоманду, план 67)
	MaxWallClock time.Duration // 0 = без лимита времени
	MaxLoopIters int           // 0 = дефолт (maxWhileIter)
}

// RestrictedProfile — строгий профиль для недоверенного кода (ИИ/marketplace).
func RestrictedProfile() SandboxProfile {
	return SandboxProfile{
		AllowNet:     false,
		AllowFile:    false,
		MaxWallClock: 10 * time.Second,
		MaxLoopIters: 1_000_000,
	}
}

// Vars возвращает extraVars, навязывающие запреты возможностей профиля
// (сеть/email/файлы). Мержить ПОСЛЕ обычных переменных запуска, чтобы deny-
// guard'ы перекрыли стандартные функции. Разрешённые возможности не внедряются —
// остаются обычные функции (с глобальным предохранителем сети, план 62).
func (p SandboxProfile) Vars() map[string]any {
	m := map[string]any{}
	if !p.AllowNet {
		deny := NetGuard(func() error {
			return errors.New("сеть запрещена в этом режиме (песочница)")
		})
		for k, v := range NewHTTPFunctions(deny) {
			m[k] = v
		}
		// nil sender: deny-guard срабатывает (checkNet) раньше обращения к
		// отправителю, поэтому реальный EmailSender здесь не нужен.
		for k, v := range NewEmailFunctions(nil, deny) {
			m[k] = v
		}
	}
	if !p.AllowFile {
		deny := FileGuard(func() error {
			return errors.New("файловые операции запрещены в этом режиме (песочница)")
		})
		for k, v := range NewFileFunctions(deny) {
			m[k] = v
		}
	}
	if !p.AllowExec {
		// Команды ОС — строго опаснее сети/файлов (RCE), поэтому всегда
		// запрещены недоверенному коду (RestrictedProfile: AllowExec=false).
		deny := ExecGuard(func() error {
			return errors.New("выполнение команд ОС запрещено в этом режиме (песочница)")
		})
		for k, v := range NewExecFunctions(deny, nil) {
			m[k] = v
		}
	}
	// ИИ-builtin'ы (llm_builtins.go) ходят в сеть (ai.Ask), а РаспознатьДокумент
	// ещё и читает файл с диска ДО сетевого вызова. Они внедряются через
	// dslvars.Build(), но не входят в HTTP/Email/File-наборы выше — поэтому
	// перекрываем их отдельно, иначе они стали бы дырой в границе песочницы.
	if !p.AllowNet {
		deny := llmDenyFn("ИИ-запросы запрещены в этом режиме (песочница)")
		for k := range NewLLMFunctions(nil) {
			m[k] = deny
		}
	}
	if !p.AllowFile {
		// РаспознатьДокумент/RecognizeDocument читают файл — закрываем и при
		// запрете только файлов (профиль AllowNet=true, AllowFile=false).
		deny := llmDenyFn("файловые операции запрещены в этом режиме (песочница)")
		m["РаспознатьДокумент"] = deny
		m["RecognizeDocument"] = deny
	}
	return m
}

// llmDenyFn — заглушка ИИ-builtin'а, мгновенно запрещающая вызов (userError,
// ловится Попыткой). Песочница перекрывает ею ИИ-функции, которые ходят в сеть
// и читают файлы.
func llmDenyFn(msg string) BuiltinFunc {
	return func(args []any, file string, line int) (any, error) {
		panic(userError{Msg: msg})
	}
}

// RunSandboxed исполняет процедуру с ресурсными лимитами профиля (wall-clock и
// итерации). Запреты возможностей (сеть/файлы/ИИ) подаются вызывающим через
// extraVars: передайте p.Vars() ПОСЛЕ обычных переменных запуска, иначе
// возможности не будут ограничены. Возвращаемое значение — в result.
func (i *Interpreter) RunSandboxed(proc *ast.ProcedureDecl, this This, p SandboxProfile, result *any, extraVars ...map[string]any) (err error) {
	e := i.startEnv(this)
	if p.MaxWallClock > 0 {
		e.ec.deadline = time.Now().Add(p.MaxWallClock)
	}
	e.ec.maxLoopIters = p.MaxLoopIters
	defer func() {
		if r := recover(); r != nil {
			switch s := r.(type) {
			case dslStop:
				err = s.err
			case userError:
				err = &DSLError{File: e.ec.curFile, Line: e.ec.curLine, Msg: s.Msg, Err: s.Err}
			case dslReturn:
				if result != nil {
					*result = s.val
				}
			default:
				panic(r)
			}
		}
	}()
	for _, m := range extraVars {
		for k, v := range m {
			e.set(k, v)
		}
	}
	i.execBlock(proc.Body, e)
	return nil
}
