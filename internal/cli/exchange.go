package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/ivantit66/onebase/internal/exchange"
	"github.com/ivantit66/onebase/internal/metadata"
	"github.com/ivantit66/onebase/internal/project"
	"github.com/ivantit66/onebase/internal/runtime"
	"github.com/ivantit66/onebase/internal/storage"
	"github.com/spf13/cobra"
)

var exchangeCmd = &cobra.Command{
	Use:   "exchange",
	Short: "Обмен данными между базами OneBase (планы обмена, план 86)",
	Long: `Файловый обмен данными между базами OneBase по плану обмена.

Цикл: на каждой базе задаётся её узел (exchange init), затем изменения
выгружаются в файл пакета (.obx) для узла-получателя (exchange dump) и
загружаются на приёмнике (exchange load). Повторная загрузка идемпотентна.`,
}

var exchangeInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Задать код текущего узла базы для плана обмена",
	RunE:  runExchangeInit,
}

var exchangeDumpCmd = &cobra.Command{
	Use:   "dump",
	Short: "Выгрузить изменения для узла-получателя в файл пакета (.obx)",
	RunE:  runExchangeDump,
}

var exchangeLoadCmd = &cobra.Command{
	Use:   "load",
	Short: "Загрузить пакет обмена (.obx) в базу",
	RunE:  runExchangeLoad,
}

var exchangeStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Показать состояние обмена по плану: очередь и счётчики по узлам",
	RunE:  runExchangeStatus,
}

func init() {
	addBaseFlags(exchangeInitCmd)
	exchangeInitCmd.Flags().String("plan", "", "имя плана обмена (обязательно)")
	exchangeInitCmd.Flags().String("node", "", "код текущего узла (обязательно)")

	addBaseFlags(exchangeDumpCmd)
	exchangeDumpCmd.Flags().String("plan", "", "имя плана обмена (обязательно)")
	exchangeDumpCmd.Flags().String("to", "", "код узла-получателя (обязательно)")
	exchangeDumpCmd.Flags().String("out", "", "путь к файлу пакета .obx (обязательно)")

	addBaseFlags(exchangeLoadCmd)
	exchangeLoadCmd.Flags().String("in", "", "путь к файлу пакета .obx (обязательно)")

	addBaseFlags(exchangeStatusCmd)
	exchangeStatusCmd.Flags().String("plan", "", "имя плана обмена (обязательно)")

	exchangeCmd.AddCommand(exchangeInitCmd, exchangeDumpCmd, exchangeLoadCmd, exchangeStatusCmd)
	rootCmd.AddCommand(exchangeCmd)
}

// openExchangeBase разрешает базу, открывает БД и грузит конфигурацию — общая
// часть всех подкоманд exchange.
func openExchangeBase(cmd *cobra.Command) (*baseConfig, *storageAndProject, error) {
	bc, err := resolveBase(cmd)
	if err != nil {
		return nil, nil, err
	}
	ctx := context.Background()
	db, err := bc.OpenDB(ctx)
	if err != nil {
		bc.Cleanup()
		return nil, nil, err
	}
	proj, err := project.Load(bc.Dir)
	if err != nil {
		db.Close()
		bc.Cleanup()
		return nil, nil, fmt.Errorf("load project: %w", err)
	}
	if err := db.EnsureExchangeSchema(ctx); err != nil {
		proj.Close()
		db.Close()
		bc.Cleanup()
		return nil, nil, err
	}
	return bc, &storageAndProject{db: db, proj: proj, ctx: ctx}, nil
}

type storageAndProject struct {
	db   *storage.DB
	proj *project.Project
	ctx  context.Context
}

func (sp *storageAndProject) Close() {
	sp.proj.Close()
	sp.db.Close()
}

// resolver строит реестр с сущностями конфигурации — достаточно для GetEntity,
// который нужен сборке/загрузке пакета.
func (sp *storageAndProject) resolver() *runtime.Registry {
	reg := runtime.NewRegistry()
	reg.Load(runtime.LoadOptions{Entities: sp.proj.Entities})
	return reg
}

func runExchangeInit(cmd *cobra.Command, _ []string) error {
	planName, _ := cmd.Flags().GetString("plan")
	node, _ := cmd.Flags().GetString("node")
	if planName == "" || node == "" {
		return fmt.Errorf("укажите --plan <имя плана> и --node <код узла>")
	}
	bc, sp, err := openExchangeBase(cmd)
	if err != nil {
		return err
	}
	defer bc.Cleanup()
	defer sp.Close()

	plan := findExchangePlan(sp.proj, planName)
	if plan == nil {
		return fmt.Errorf("план обмена %q не найден в конфигурации", planName)
	}
	if plan.Node(node) == nil {
		return fmt.Errorf("узел %q не описан в плане %q (узлы: %s)", node, plan.Name, nodeCodes(plan))
	}
	if err := sp.db.SaveExchangeThisNode(sp.ctx, plan.Name, node); err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "Текущий узел для плана %q: %s\n", plan.Name, node)
	return nil
}

func runExchangeDump(cmd *cobra.Command, _ []string) error {
	planName, _ := cmd.Flags().GetString("plan")
	to, _ := cmd.Flags().GetString("to")
	out, _ := cmd.Flags().GetString("out")
	if planName == "" || to == "" || out == "" {
		return fmt.Errorf("укажите --plan <имя>, --to <код узла> и --out <файл>")
	}
	bc, sp, err := openExchangeBase(cmd)
	if err != nil {
		return err
	}
	defer bc.Cleanup()
	defer sp.Close()

	plan := findExchangePlan(sp.proj, planName)
	if plan == nil {
		return fmt.Errorf("план обмена %q не найден в конфигурации", planName)
	}
	if plan.Node(to) == nil {
		return fmt.Errorf("узел-получатель %q не описан в плане %q (узлы: %s)", to, plan.Name, nodeCodes(plan))
	}
	data, err := exchange.BuildPackage(sp.ctx, sp.db, sp.resolver(), plan, to)
	if err != nil {
		return err
	}
	if err := os.WriteFile(out, data, 0o644); err != nil {
		return fmt.Errorf("запись пакета: %w", err)
	}
	pkg, _ := exchange.ParsePackage(data)
	fmt.Fprintf(os.Stdout, "План %q → узел %q: выгружено объектов %d (сообщение №%d) в %s\n",
		plan.Name, to, len(pkg.Objects), pkg.MessageNo, out)
	return nil
}

func runExchangeLoad(cmd *cobra.Command, _ []string) error {
	in, _ := cmd.Flags().GetString("in")
	if in == "" {
		return fmt.Errorf("укажите --in <файл пакета>")
	}
	data, err := os.ReadFile(in)
	if err != nil {
		return fmt.Errorf("чтение пакета: %w", err)
	}
	pkg, err := exchange.ParsePackage(data)
	if err != nil {
		return err
	}
	bc, sp, err := openExchangeBase(cmd)
	if err != nil {
		return err
	}
	defer bc.Cleanup()
	defer sp.Close()

	plan := findExchangePlan(sp.proj, pkg.Plan)
	if plan == nil {
		return fmt.Errorf("план обмена %q из пакета не найден в конфигурации приёмника", pkg.Plan)
	}
	// Headless-загрузка без интерпретатора: правило hook (если задано) откатится
	// к by_time внутри движка.
	res, err := exchange.ApplyPackage(sp.ctx, sp.db, sp.resolver(), plan, data, exchange.ApplyOptions{})
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "Пакет плана %q от узла %q (сообщение №%d): применено %d, пропущено %d, удалено %d, конфликтов %d\n",
		plan.Name, pkg.FromNode, pkg.MessageNo, res.Applied, res.Skipped, res.Deleted, res.Conflicts)
	return nil
}

func runExchangeStatus(cmd *cobra.Command, _ []string) error {
	planName, _ := cmd.Flags().GetString("plan")
	if planName == "" {
		return fmt.Errorf("укажите --plan <имя плана>")
	}
	bc, sp, err := openExchangeBase(cmd)
	if err != nil {
		return err
	}
	defer bc.Cleanup()
	defer sp.Close()

	plan := findExchangePlan(sp.proj, planName)
	if plan == nil {
		return fmt.Errorf("план обмена %q не найден в конфигурации", planName)
	}
	thisNode, _ := sp.db.GetExchangeThisNode(sp.ctx, plan.Name)
	counts, err := sp.db.ExchangePendingCounts(sp.ctx, plan.Name)
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stdout, "План обмена: %s\n", plan.Name)
	if thisNode == "" {
		fmt.Fprintln(os.Stdout, "Текущий узел: не задан — выполните `onebase exchange init`")
	} else {
		fmt.Fprintf(os.Stdout, "Текущий узел: %s\n", thisNode)
	}
	fmt.Fprintln(os.Stdout, "\nУзлы (очередь = ждут выгрузки; отпр./подтв. — номера сообщений):")
	for _, n := range plan.Nodes {
		peer, _ := sp.db.GetExchangePeer(sp.ctx, plan.Name, n.Code)
		mark := ""
		if thisNode != "" && strings.EqualFold(n.Code, thisNode) {
			mark = " ← этот узел"
		}
		fmt.Fprintf(os.Stdout, "  %-10s %-22s очередь=%d отпр.=%d подтв.=%d принято=%d%s\n",
			n.Code, n.Name, counts[n.Code], peer.SentNo, peer.AckNo, peer.RecvNo, mark)
	}
	return nil
}

func findExchangePlan(proj *project.Project, name string) *metadata.ExchangePlan {
	for _, p := range proj.ExchangePlans {
		if strings.EqualFold(p.Name, name) {
			return p
		}
	}
	return nil
}

func nodeCodes(plan *metadata.ExchangePlan) string {
	codes := make([]string, 0, len(plan.Nodes))
	for _, n := range plan.Nodes {
		codes = append(codes, n.Code)
	}
	return strings.Join(codes, ", ")
}
