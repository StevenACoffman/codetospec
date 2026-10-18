// Package config resolves the CLI configuration from flags, the optional
// YAML config file and environment variables, in that precedence order.
package config

import (
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/StevenACoffman/codetospec/internal/extract"
	"gopkg.in/yaml.v3"
)

// Extractor configures one external facts extractor command.
type Extractor struct {
	Name    string   `yaml:"name"`
	Cmd     string   `yaml:"cmd"`
	Args    []string `yaml:"args"` // "{src}" is substituted with --src
	Timeout string   `yaml:"timeout"`
}

// Defaults of the run flags.
const (
	defaultExtractorTimeout = 300 * time.Second
	defaultWorkers          = 4
	defaultMaxTokens        = 4096
	defaultReduceBatch      = 30
)

// TimeoutDuration parses the configured timeout, defaulting to 300s.
func (e Extractor) TimeoutDuration() (time.Duration, error) {
	if e.Timeout == "" {
		return defaultExtractorTimeout, nil
	}
	d, err := time.ParseDuration(e.Timeout)
	if err != nil {
		return 0, fmt.Errorf("extractor %s: invalid timeout %q: %w", e.Name, e.Timeout, err)
	}
	return d, nil
}

// fileConfig models codetospec.yaml; every field is optional.
type fileConfig struct {
	Exclude        []string    `yaml:"exclude"`
	Extractors     []Extractor `yaml:"extractors"`
	FactsFiles     []string    `yaml:"facts_files"`
	DomainStrategy string      `yaml:"domain_strategy"`
	DomainDepth    int         `yaml:"domain_depth"`
	// DomainModels are modelith model files, relative to --src.
	DomainModels        []string          `yaml:"domain_models"`
	DomainModelEntities map[string]string `yaml:"domain_model_entities"`
	WriteDomainModel    bool              `yaml:"write_domain_model"`
}

// Config is the resolved configuration for one command invocation.
type Config struct {
	Src string
	// RefRoots are the extra source trees extractor rules may cite, by
	// root name (--ref-root name=dir): webapp's shared pkg/ for a service.
	RefRoots       map[string]string
	Out            string
	BaseURL        string
	APIKey         string
	Model          string
	Lang           string
	Workers        int
	MaxTokens      int
	Exclude        []string
	FactsFiles     []string
	LogLevel       string
	Extractors     []Extractor
	DomainStrategy string
	DomainDepth    int
	NoTUI          bool
	Crosscheck     bool
	Repair         bool
	Reground       bool
	ReduceBatch    int
	ReduceMode     string // "full" or "merge" (see reducer.ModeMerge)
	Format         string // drift output format: "text" | "json"
	// DomainModels are human modelith models (*.modelith.yaml) the graph is
	// compared with (--domain-model, then domain_models resolved against
	// --src); DomainModelEntities maps a model entity to the code entity it
	// is, for names that differ (see domainmodel.Options).
	DomainModels        []string
	DomainModelEntities map[string]string
	// WriteDomainModel writes modelith models under <out>/models: the human
	// models enhanced with what the code shows, and generated ones for the
	// entities they do not name (--write-domain-model).
	WriteDomainModel bool
}

// DefaultExclude lists the directories ignored by default.
func DefaultExclude() []string {
	return []string{"vendor", "node_modules", "storage", "dist", "build", ".git"}
}

// stringList is a repeatable string flag.
type stringList []string

func (l *stringList) String() string { return strings.Join(*l, ",") }

func (l *stringList) Set(v string) error {
	*l = append(*l, v)
	return nil
}

// refRoots is the --ref-root flag: name=dir pairs, repeatable.
type refRoots map[string]string

func (r refRoots) String() string {
	pairs := make([]string, 0, len(r))
	for name, dir := range r {
		pairs = append(pairs, name+"="+dir)
	}
	slices.Sort(pairs)
	return strings.Join(pairs, ",")
}

func (r refRoots) Set(v string) error {
	name, dir, ok := strings.Cut(v, "=")
	switch {
	case !ok || name == "" || dir == "":
		return fmt.Errorf("--ref-root %q: want name=dir", v)
	case r[name] != "":
		return fmt.Errorf("--ref-root %q: root %q given twice", v, name)
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return fmt.Errorf("--ref-root %q: %q is not a readable directory", v, dir)
	}
	r[name] = dir
	return nil
}

// registerRefRoots adds --ref-root to a command that reads citations.
func registerRefRoots(flags *flag.FlagSet, cfg *Config) {
	cfg.RefRoots = make(map[string]string)
	flags.Var(refRoots(cfg.RefRoots), "ref-root",
		"name=dir: an extra source tree extractor rules cite (repeatable)")
}

// Roots resolves citations: --src, and the --ref-root trees by name.
func (c *Config) Roots() extract.Roots {
	extra := make(map[string]fs.FS, len(c.RefRoots))
	for name, dir := range c.RefRoots {
		extra[name] = os.DirFS(dir)
	}
	return extract.Roots{Src: os.DirFS(c.Src), Extra: extra}
}

// ParseRun resolves the run command configuration.
func ParseRun(args []string) (*Config, error) {
	flags := flag.NewFlagSet("run", flag.ContinueOnError)
	cfg := &Config{}
	var (
		configPath   string
		excludeCSV   string
		facts        stringList
		domainModels stringList
	)
	flags.StringVar(&cfg.Src, "src", "", "root of the analyzed repository (required)")
	registerRefRoots(flags, cfg)
	flags.StringVar(&cfg.Out, "out", "", "output graph directory (required)")
	flags.StringVar(
		&configPath,
		"config",
		"",
		"config file (default <src>/codetospec.yaml when present)",
	)
	flags.StringVar(
		&cfg.BaseURL,
		"base-url",
		os.Getenv("LLM_BASE_URL"),
		"OpenAI-compatible endpoint",
	)
	flags.StringVar(
		&cfg.APIKey,
		"api-key",
		os.Getenv("LLM_API_KEY"),
		"API key (empty accepted for local vLLM)",
	)
	flags.StringVar(&cfg.Model, "model", os.Getenv("LLM_MODEL"), "model name")
	flags.StringVar(&cfg.Lang, "lang", "fr", "requirements language (fr or en)")
	flags.IntVar(&cfg.Workers, "workers", defaultWorkers, "map parallelism")
	flags.IntVar(&cfg.MaxTokens, "max-tokens", defaultMaxTokens, "generation cap per LLM call")
	flags.StringVar(
		&excludeCSV,
		"exclude",
		strings.Join(DefaultExclude(), ","),
		"comma-separated ignored directories",
	)
	flags.Var(&facts, "facts", "additional facts JSON file (repeatable)")
	flags.BoolVar(&cfg.WriteDomainModel, "write-domain-model", false,
		"write modelith models under <out>/models: human models enhanced, and generated ones")
	flags.Var(&domainModels, "domain-model",
		"human modelith model (*.modelith.yaml) to compare the graph with (repeatable)")
	flags.StringVar(&cfg.LogLevel, "log-level", "info", "debug, info, warn or error")
	flags.BoolVar(&cfg.NoTUI, "no-tui", false, "disable the full-screen dashboard, log to stderr")
	registerPassFlags(flags, cfg)
	flags.IntVar(
		&cfg.DomainDepth,
		"domain-depth",
		1,
		"namespace segments (after the root) forming a domain; 2-3 splits a single-root repo (e.g. core-controller-sdk)",
	)
	if err := flags.Parse(args); err != nil {
		return nil, err
	}

	if cfg.Src == "" || cfg.Out == "" {
		return nil, errors.New("run requires --src and --out")
	}
	if cfg.Repair {
		cfg.Crosscheck = true
	}
	explicit := map[string]bool{}
	flags.Visit(func(f *flag.Flag) { explicit[f.Name] = true })

	fileCfg, err := loadFile(configPath, cfg.Src)
	if err != nil {
		return nil, err
	}
	cfg.merge(
		&fileCfg,
		explicit,
		&cliLists{exclude: excludeCSV, facts: facts, models: domainModels},
	)
	if err = cfg.validateRun(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// cliLists are the list flags of the run command, before merging.
type cliLists struct {
	exclude       string // comma-separated
	facts, models []string
}

// merge completes cfg from the config file: a flag given on the command
// line wins over the file (explicit), list flags are extended by the file's
// entries, and the file's model paths are relative to --src.
func (c *Config) merge(fileCfg *fileConfig, explicit map[string]bool, lists *cliLists) {
	c.Exclude = splitCSV(lists.exclude)
	if !explicit["exclude"] && len(fileCfg.Exclude) > 0 {
		c.Exclude = fileCfg.Exclude
	}
	c.FactsFiles = append([]string(nil), lists.facts...)
	for _, f := range fileCfg.FactsFiles {
		if !slices.Contains(c.FactsFiles, f) {
			c.FactsFiles = append(c.FactsFiles, f)
		}
	}
	c.DomainModels = append([]string(nil), lists.models...)
	for _, m := range fileCfg.DomainModels {
		if !filepath.IsAbs(m) {
			m = filepath.Join(c.Src, m)
		}
		if !slices.Contains(c.DomainModels, m) {
			c.DomainModels = append(c.DomainModels, m)
		}
	}
	c.DomainModelEntities = fileCfg.DomainModelEntities
	if !explicit["write-domain-model"] && fileCfg.WriteDomainModel {
		c.WriteDomainModel = true
	}
	c.Extractors = fileCfg.Extractors
	c.DomainStrategy = fileCfg.DomainStrategy
	if c.DomainStrategy == "" {
		c.DomainStrategy = "auto"
	}
	if !explicit["domain-depth"] && fileCfg.DomainDepth > 0 {
		c.DomainDepth = fileCfg.DomainDepth
	}
	if c.DomainDepth < 1 {
		c.DomainDepth = 1
	}
}

// ParseVerify resolves the verify command configuration.
func ParseVerify(args []string) (*Config, error) {
	flags := flag.NewFlagSet("verify", flag.ContinueOnError)
	cfg := &Config{}
	flags.StringVar(&cfg.Src, "src", "", "root of the analyzed repository (required)")
	registerRefRoots(flags, cfg)
	flags.StringVar(&cfg.Out, "out", "", "graph directory to verify (required)")
	flags.StringVar(&cfg.LogLevel, "log-level", "info", "debug, info, warn or error")
	if err := flags.Parse(args); err != nil {
		return nil, err
	}
	if cfg.Src == "" || cfg.Out == "" {
		return nil, errors.New("verify requires --src and --out")
	}
	return cfg, nil
}

// ParseDrift resolves the drift command configuration (verify flags plus an
// output format for CI).
func ParseDrift(args []string) (*Config, error) {
	flags := flag.NewFlagSet("drift", flag.ContinueOnError)
	cfg := &Config{}
	flags.StringVar(&cfg.Src, "src", "", "root of the analyzed repository (required)")
	registerRefRoots(flags, cfg)
	flags.StringVar(&cfg.Out, "out", "", "graph directory to check (required)")
	flags.StringVar(&cfg.Format, "format", "text", "output format: text or json")
	flags.StringVar(&cfg.LogLevel, "log-level", "info", "debug, info, warn or error")
	if err := flags.Parse(args); err != nil {
		return nil, err
	}
	if cfg.Src == "" || cfg.Out == "" {
		return nil, errors.New("drift requires --src and --out")
	}
	if cfg.Format != "text" && cfg.Format != "json" {
		return nil, fmt.Errorf("--format %q must be text or json", cfg.Format)
	}
	return cfg, nil
}

// ParseStats resolves the stats command configuration.
func ParseStats(args []string) (*Config, error) {
	flags := flag.NewFlagSet("stats", flag.ContinueOnError)
	cfg := &Config{}
	flags.StringVar(&cfg.Out, "out", "", "graph directory (required)")
	flags.StringVar(&cfg.LogLevel, "log-level", "info", "debug, info, warn or error")
	if err := flags.Parse(args); err != nil {
		return nil, err
	}
	if cfg.Out == "" {
		return nil, errors.New("stats requires --out")
	}
	return cfg, nil
}

func (c *Config) validateRun() error {
	if info, err := os.Stat(c.Src); err != nil || !info.IsDir() {
		return fmt.Errorf("--src %q is not a readable directory", c.Src)
	}
	if c.Lang != "fr" && c.Lang != "en" {
		return fmt.Errorf("--lang %q must be fr or en", c.Lang)
	}
	if c.ReduceMode != "full" && c.ReduceMode != "merge" {
		return fmt.Errorf("--reduce-mode %q must be full or merge", c.ReduceMode)
	}
	if c.Workers < 1 {
		return fmt.Errorf("--workers %d must be >= 1", c.Workers)
	}
	if c.MaxTokens < 1 {
		return fmt.Errorf("--max-tokens %d must be >= 1", c.MaxTokens)
	}
	switch c.DomainStrategy {
	case "auto", "namespace", "directory":
	default:
		return fmt.Errorf(
			"domain_strategy %q must be auto, namespace or directory",
			c.DomainStrategy,
		)
	}
	if c.BaseURL == "" {
		return errors.New("missing LLM endpoint: set --base-url or LLM_BASE_URL")
	}
	if c.Model == "" {
		return errors.New("missing LLM model: set --model or LLM_MODEL")
	}
	for _, e := range c.Extractors {
		if e.Name == "" || e.Cmd == "" {
			return fmt.Errorf(
				"extractor entries need both name and cmd (got name=%q cmd=%q)",
				e.Name,
				e.Cmd,
			)
		}
		if _, err := e.TimeoutDuration(); err != nil {
			return err
		}
	}
	return nil
}

// loadFile reads the YAML config file: the explicit --config path, or
// <src>/codetospec.yaml when present.
func loadFile(configPath, src string) (fileConfig, error) {
	var fc fileConfig
	path := configPath
	if path == "" {
		candidate := filepath.Join(src, "codetospec.yaml")
		if _, err := os.Stat(candidate); err == nil {
			path = candidate
		} else if !errors.Is(err, fs.ErrNotExist) {
			return fc, fmt.Errorf("stat %s: %w", candidate, err)
		}
	}
	if path == "" {
		return fc, nil
	}
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return fc, fmt.Errorf("read config %s: %w", path, err)
	}
	if err = yaml.Unmarshal(data, &fc); err != nil {
		return fc, fmt.Errorf("parse config %s: %w", path, err)
	}
	return fc, nil
}

func splitCSV(s string) []string {
	var out []string
	for part := range strings.SplitSeq(s, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

// registerPassFlags declares the flags of the LLM passes: crosscheck and
// repair, reground, and how reduce batches and consolidates.
func registerPassFlags(flags *flag.FlagSet, cfg *Config) {
	flags.BoolVar(
		&cfg.Crosscheck,
		"crosscheck",
		false,
		"adversarial review pass: a fresh-context LLM tries to refute each rule against its cited lines",
	)
	flags.BoolVar(
		&cfg.Repair,
		"repair",
		false,
		"repair pass (implies --crosscheck): a flagged rule re-cites the exact span of a precise symbol; needs a SCIP index",
	)
	flags.BoolVar(
		&cfg.Reground,
		"reground",
		false,
		"give cached map outputs with ungrounded rules one correction exchange (once per output; costs one LLM call each)",
	)
	flags.IntVar(
		&cfg.ReduceBatch,
		"reduce-batch",
		defaultReduceBatch,
		"max candidate rules per reduce call; larger domains are batched then merged",
	)
	flags.StringVar(
		&cfg.ReduceMode,
		"reduce-mode",
		"full",
		"full: the LLM consolidates every rule (rationale, acceptance criteria); "+
			"merge: only candidates with a near-duplicate go to the LLM, the rest are kept as mapped (much cheaper)",
	)
}
