package config

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/yangzhuangqiu/gobase/config/provider"
)

var defaultConfigFiles = []string{
	"application.yaml",
	"application.yml",
	"config/application.yaml",
	"config/application.yml",
}

const (
	// 默认层优先级：低于 file/remote/pinned。
	defaultLayerPriority = 100
	// 文件层优先级：高于 default，低于 remote/pinned。
	fileLayerPriority = 200
	// 远端层基础优先级：实际层级为 base + source.priority。
	remoteLayerPriorityBase = 300
	// pinned 层优先级：用于运行时强覆盖。
	pinnedLayerPriority = 10_000

	// 远端 source.priority 代码硬约束区间（含边界）。
	remoteSourcePriorityMin = 0
	remoteSourcePriorityMax = 1_000

	// 本地配置文件监听默认轮询间隔（当显式参数与文件配置都不可用时）。
	defaultLocalFileWatchInterval = 30 * time.Second
	// 本地配置文件监听轮询间隔最小值（含边界）。
	localFileWatchIntervalMin = 1 * time.Second
	// 本地配置文件监听轮询间隔最大值（含边界）。
	localFileWatchIntervalMax = 60 * time.Minute
)

type runtimePlan struct {
	// fileLayers 是启动阶段读取并移除 bootstrap 后的有序文件层。
	fileLayers []runtimeFileLayer
	// configFiles 是 Build 后固定的绝对路径列表，供 watch refresh 全量重读。
	configFiles []string
	// localFileWatchInterval 是全部本地文件共享的轮询周期。
	localFileWatchInterval time.Duration
	// pinnedValues 是最终高优先级固定覆盖层。
	pinnedValues map[string]any
	// remoteSessions 是 Provider 打开并校验后的稳定运行会话。
	remoteSessions []remoteSessionPlan
}

// remoteSessionPlan 保存内核运行所需的 Provider 会话及中立 Source 视图。
type remoteSessionPlan struct {
	// provider 是错误归因和 Explain 使用的稳定 Provider 名称。
	provider string
	// session 同时提供加载能力并持有连接资源。
	session provider.Session
	// sources 固化 Open 阶段读取的 Source 元信息，避免运行期语义漂移。
	sources []remoteSource
}

// remoteSource 绑定厂商 Source 与一次性捕获的中立元信息。
type remoteSource struct {
	// source 是回传给所属 Session 的不透明配置源对象。
	source provider.Source
	// info 是排序、容错、日志和 Explain 使用的稳定元信息。
	info provider.SourceInfo
}

// providerCallPanicError 标记 Provider SPI 调用发生的编程异常。
// 该标记用于阻止 Optional Source 把 panic 当作可降级的普通远端失败吞掉。
type providerCallPanicError struct {
	// stage 是发生 panic 的 Provider SPI 调用阶段。
	stage string
	// payloadType 是 panic payload 的动态类型；不保留原值，避免敏感信息进入错误链。
	payloadType string
}

// Error 返回带阶段上下文的稳定 panic 诊断文本。
func (this *providerCallPanicError) Error() string {
	return fmt.Sprintf("stage=%s reason=provider_call_panic payload_type=%s", this.stage, this.payloadType)
}

// newProviderCallPanicError 生成不携带 panic 原值的 Provider SPI 异常。
func newProviderCallPanicError(stage string, payload any) *providerCallPanicError {
	return &providerCallPanicError{stage: stage, payloadType: fmt.Sprintf("%T", payload)}
}

// runtimeFileLayer 描述一个已解析的有序本地配置层。
type runtimeFileLayer struct {
	// path 是已校验的绝对文件路径，用于刷新重读和 Explain 来源。
	path string
	// data 是移除 bootstrap 根块后的业务配置内容。
	data map[string]any
}

type remoteStageSet struct {
	watch        string
	watchRefresh string
	loadLayer    string
	parseLayer   string
}

// Builder 负责组装配置来源、优先级和监听策略。
type Builder struct {
	defaults               any
	fileDeclared           bool
	configFiles            []string
	disableConfigFile      bool
	enableLocalFileWatch   bool
	localFileWatchInterval time.Duration
	localFileWatchExplicit bool
	bootstrapEnvPrefix     string
	bootstrapOverrides     map[string]any
	inputErr               error
	pinnedValues           map[string]any
	providers              map[string]provider.Provider
	snapshotValidators     []SnapshotValidator
	onRefreshErr           func(error)
}

// New 根据声明式 Option 创建配置构建器，不获取文件、远端连接或 watcher 资源。
//
// 典型调用场景：调用方在装配边界声明稳定配置来源，再调用 Build 创建独立 Manager。
//
// 参数：
// - options：按输入顺序应用；同类覆盖项以后声明为准，追加项保持声明顺序。
//
// 返回：
// - Builder：只保存声明的构建器；声明错误延迟到 Build fail-fast 返回。
func New(options ...Option) *Builder {
	// 阶段一：创建每次声明独占的 Provider 索引和空构建状态。
	result := &Builder{
		providers: make(map[string]provider.Provider),
	}
	// 阶段二：稳定重放声明；Option 只写内存，不提前取得运行资源。
	for _, option := range options {
		if option != nil {
			option.apply(result)
		}
	}
	return result
}

// withDefaults 设置默认配置层。
func (this *Builder) withDefaults(cfg any) *Builder {
	this.defaults = cfg
	return this
}

// withConfigFile 设置启动阶段单个显式配置文件路径。
func (this *Builder) withConfigFile(path string) *Builder {
	path = strings.TrimSpace(path)
	this.configFiles = this.configFiles[:0]
	if path != "" {
		this.configFiles = append(this.configFiles, path)
	}
	return this
}

// withoutConfigFile 显式禁用配置文件加载与默认发现。
func (this *Builder) withoutConfigFile() *Builder {
	this.disableConfigFile = true
	return this
}

// withLocalFileWatch 启用本地配置文件变更监听（轮询模式）。
//
// 语义：
// 1. 仅当最终解析出的 configFiles 非空时生效；
// 2. 监听到任一文件元信息变化后触发一次全量 refresh；
// 3. refresh 失败不会中断进程，保留旧快照，并通过 OnRefreshError 上报。
//
// 参数：
// - interval: 可选轮询间隔；当传入 >0 时强制使用该值；
// - 未显式传入 interval 时，尝试读取配置文件中的 bootstrap.configFile.watchInterval；
// - 文件未配置或值非法时回退默认值（30s）。
func (this *Builder) withLocalFileWatch(interval ...time.Duration) *Builder {
	this.enableLocalFileWatch = true
	this.localFileWatchInterval = 0
	this.localFileWatchExplicit = false
	if len(interval) > 0 && interval[0] > 0 {
		this.localFileWatchInterval = interval[0]
		this.localFileWatchExplicit = true
	}
	return this
}

// withFile 应用一个完整文件来源声明。
// 阶段角色：Option 应用期统一收口 path/enabled/watch，避免三个顶层声明产生隐式组合。
func (this *Builder) withFile(disabled bool, paths []string, watch bool, interval time.Duration) *Builder {
	if this.fileDeclared {
		this.recordInputError(errorForConfigConfigFileInvalid(messagef("op=config.file result=failed stage=declare reason=duplicate_declaration")))
		return this
	}
	this.fileDeclared = true
	return this.applyFile(disabled, paths, true, watch, interval)
}

// withInputFile 应用单次 Build 文件覆盖，不参与稳定 File 声明重复检测。
// 阶段角色：Application 声明完成后的 CLI/宿主输入层。
func (this *Builder) withInputFile(disabled bool, paths []string, pathsSet bool, watch bool, interval time.Duration) *Builder {
	return this.applyFile(disabled, paths, pathsSet, watch, interval)
}

// applyFile 把已归一化文件状态写入 Builder。
// 阶段角色：稳定 File 与单次 Input 的统一状态收口。
func (this *Builder) applyFile(disabled bool, paths []string, pathsSet bool, watch bool, interval time.Duration) *Builder {
	// 禁用与显式路径或监听互斥，提前记录稳定、可分支的声明错误。
	if disabled {
		if len(paths) > 0 || watch {
			this.recordInputError(errorForConfigConfigFileDisabledConflict(messagef("op=config.file result=failed stage=declare reason=disabled_with_path_or_watch")))
		}
		return this.withoutConfigFile()
	}

	if pathsSet {
		this.configFiles = append(this.configFiles[:0], paths...)
	}
	if watch {
		if interval > 0 {
			return this.withLocalFileWatch(interval)
		}
		return this.withLocalFileWatch()
	}
	return this
}

// withBootstrapEnvPrefix 设置启动阶段环境变量前缀。
func (this *Builder) withBootstrapEnvPrefix(prefix string) *Builder {
	this.bootstrapEnvPrefix = strings.TrimSpace(prefix)
	return this
}

// withBootstrapOverrides 合并启动阶段覆盖值。
func (this *Builder) withBootstrapOverrides(values map[string]any) *Builder {
	normalized := normalizeInputValues(values, true)
	this.bootstrapOverrides = mergeMapValues(this.bootstrapOverrides, normalized)
	return this
}

// withPinnedValues 合并运行时 pinned 层。
func (this *Builder) withPinnedValues(values map[string]any) *Builder {
	normalized := normalizeInputValues(values, false)
	this.pinnedValues = mergeMapValues(this.pinnedValues, normalized)
	return this
}

// withProvider 注册单个配置 Provider；同名声明会在 Build 阶段 fail-fast。
//
// 语义：
// 1. Provider 根据 bootstrap 动态打开 Session；
// 2. nil 输入被忽略，名称为空或重复输入会记录为构建声明错误。
func (this *Builder) withProvider(item provider.Provider) *Builder {
	if this.providers == nil {
		this.providers = make(map[string]provider.Provider)
	}
	// 声明阶段只归一化身份并聚合输入错误，不提前调用 Open 或获取远端资源。
	if item == nil {
		return this
	}
	if isNilInterface(item) {
		this.recordInputError(errorForProviderNameRequired(messagef("op=config.provider result=failed stage=register reason=provider_is_nil")))
		return this
	}
	name := strings.TrimSpace(item.Name())
	if name == "" {
		this.recordInputError(errorForProviderNameRequired(messagef("op=config.provider result=failed stage=register reason=name_required")))
		return this
	}
	if _, exists := this.providers[name]; exists {
		this.recordInputError(errorForProviderDuplicate(messagef("op=config.provider result=failed stage=register provider=%s reason=duplicate_name", name)))
		return this
	}
	this.providers[name] = item
	return this
}

// withRefreshErrorHandler 设置 watcher 触发 refresh 失败时的错误回调。
//
// 该回调用于让热更新失败可观测；未设置时保持静默兼容行为。
func (this *Builder) withRefreshErrorHandler(handler func(error)) *Builder {
	this.onRefreshErr = handler
	return this
}

// withSnapshotValidator 追加快照发布前校验器。
//
// 语义：
// 1. 在 initial install 与 refresh publish 前执行；
// 2. 任一校验器返回非 nil error 时拒绝发布新快照；
// 3. refresh 拒绝发布时保留旧快照不变。
func (this *Builder) withSnapshotValidator(validators ...SnapshotValidator) *Builder {
	for _, validator := range validators {
		if validator == nil {
			continue
		}
		this.snapshotValidators = append(this.snapshotValidators, validator)
	}
	return this
}

// Build 构建 Manager，并按需启动远端 watcher。
func (this *Builder) Build(ctx context.Context) (Manager, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if this.inputErr != nil {
		return nil, errorf("op=config.builder.build result=failed stage=normalize_builder_input reason=%w", this.inputErr)
	}

	plan, err := this.prepareRuntimePlan(ctx)
	if err != nil {
		return nil, errorf("op=config.builder.build result=failed stage=prepare_runtime_plan reason=%w", err)
	}

	manager := newDefaultManager()
	for _, remoteSession := range plan.remoteSessions {
		manager.addSession(remoteSession.session)
	}
	manager.setSnapshotValidators(this.snapshotValidators)
	rebuild := func(reloadCtx context.Context) (snapshotState, error) {
		return this.buildSnapshot(reloadCtx, plan)
	}
	manager.setRebuild(rebuild)

	initial, err := rebuild(ctx)
	if err != nil {
		buildErr := errorf("op=config.builder.build result=failed stage=build_initial_snapshot reason=%w", err)
		return nil, errors.Join(buildErr, manager.Close())
	}
	if err = manager.installInitial(initial); err != nil {
		installErr := errorf("op=config.builder.build result=failed stage=install_initial_snapshot reason=%w", err)
		return nil, errors.Join(installErr, manager.Close())
	}
	// Build context 只约束启动事务；成功后的 watcher 由 Manager 独占生命周期。
	// 保留调用链值，切断父级 deadline/cancel，避免 Manager 未关闭时 watcher 静默退出。
	watchCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	manager.setCancel(cancel)

	if err = this.startRemoteWatchers(watchCtx, manager, plan); err != nil {
		watchErr := errorf("op=config.builder.build result=failed stage=start_watchers reason=%w", err)
		return nil, errors.Join(watchErr, manager.Close())
	}
	if err = this.startLocalFileWatcher(watchCtx, manager, plan); err != nil {
		watchErr := errorf("op=config.builder.build result=failed stage=start_watchers reason=%w", err)
		return nil, errors.Join(watchErr, manager.Close())
	}

	return manager, nil
}

func (this *Builder) recordInputError(err error) {
	if err == nil {
		return
	}
	this.inputErr = errors.Join(this.inputErr, err)
}

// prepareRuntimePlan 执行启动阶段语义：bootstrap 解析、连接准备与输入固化。
func (this *Builder) prepareRuntimePlan(ctx context.Context) (runtimePlan, error) {
	if err := this.validateBootstrapSchemas(); err != nil {
		return runtimePlan{}, errorf("op=config.builder.bootstrap result=failed stage=validate_bootstrap_schema reason=%w", err)
	}

	overrides := normalizeInputValues(this.bootstrapOverrides, true)
	envValues := this.loadBootstrapEnvValues()

	configFiles, err := this.resolveEffectiveConfigFiles(overrides, envValues)
	if err != nil {
		return runtimePlan{}, err
	}

	fileLayers, fileBootstrap, err := this.loadBootstrapFileLayers(configFiles)
	if err != nil {
		return runtimePlan{}, err
	}
	localFileWatchInterval := this.resolveLocalFileWatchInterval(fileBootstrap)
	fileBootstrap = stripBootstrapConfigFileWatchBlock(fileBootstrap)

	bootstrapValues := mergeMapValues(fileBootstrap, envValues)
	bootstrapValues = mergeMapValues(bootstrapValues, overrides)
	if err := this.validateBootstrapValues(bootstrapValues); err != nil {
		return runtimePlan{}, errorf("op=config.builder.bootstrap result=failed stage=validate_bootstrap reason=%w", err)
	}

	remoteSessions, err := this.prepareRemoteSessions(ctx, bootstrapValues)
	if err != nil {
		return runtimePlan{}, errorf("op=config.builder.provider result=failed stage=prepare_connector reason=%w", err)
	}

	pinnedValues := normalizeInputValues(this.pinnedValues, false)
	delete(pinnedValues, BootstrapRootKey)

	return runtimePlan{
		fileLayers:             fileLayers,
		configFiles:            append([]string{}, configFiles...),
		localFileWatchInterval: localFileWatchInterval,
		pinnedValues:           pinnedValues,
		remoteSessions:         remoteSessions,
	}, nil
}

// validateBootstrapSchemas 对 core/provider 的 bootstrap schema 执行类型白名单校验。
func (this *Builder) validateBootstrapSchemas() error {
	if err := provider.ValidateBootstrapSchemaTypes(CoreBootstrapSchema()); err != nil {
		return errorForBootstrapInvalidType(messagef("core bootstrap schema is invalid"), err)
	}
	for _, name := range this.sortedProviderNames() {
		item := this.providers[name]
		schemaProvider, ok := item.(provider.BootstrapSchemaProvider)
		if item == nil || !ok {
			continue
		}
		if err := provider.ValidateBootstrapSchemaTypes(schemaProvider.BootstrapSchema()); err != nil {
			return errorForBootstrapInvalidType(messagef("provider bootstrap schema is invalid provider=%s", name), err)
		}
	}
	return nil
}

// resolveEffectiveConfigFiles 根据禁用策略和优先级解析最终有序配置文件列表。
func (this *Builder) resolveEffectiveConfigFiles(overrides map[string]any, envValues map[string]any) ([]string, error) {
	// 阶段一：收集稳定显式列表与两个动态单路径候选，先完成类型校验。
	explicit := append([]string{}, this.configFiles...)
	fromOverride, err := readBootstrapConfigPath(overrides)
	if err != nil {
		return nil, errorf("op=config.builder.file result=failed stage=resolve_file reason=%w", err)
	}
	fromEnv, err := readBootstrapConfigPath(envValues)
	if err != nil {
		return nil, errorf("op=config.builder.file result=failed stage=resolve_file reason=%w", err)
	}

	// 阶段二：禁用是单向收紧，与任意路径来源或监听声明同时存在都失败。
	if this.disableConfigFile {
		if len(explicit) > 0 || fromOverride != "" || fromEnv != "" || this.enableLocalFileWatch {
			return nil, errorForConfigConfigFileDisabledConflict(messagef("op=config.builder.file result=failed stage=resolve_file reason=config_file_disabled_but_explicitly_configured"))
		}
		return nil, nil
	}

	// 显式 FilePath 列表整体优先于 bootstrap override/env，并保留声明顺序。
	if len(explicit) > 0 {
		resolved, err := resolveExplicitConfigFiles(explicit)
		if err != nil {
			return nil, errorf("op=config.builder.file result=failed stage=resolve_file reason=%w", err)
		}
		return resolved, nil
	}
	candidate := fromOverride
	if candidate == "" {
		candidate = fromEnv
	}

	// 阶段三：无显式列表时只解析一个动态路径或首个默认发现候选。
	resolved, err := resolveConfigFilePath(candidate)
	if err != nil {
		return nil, errorf("op=config.builder.file result=failed stage=resolve_file reason=%w", err)
	}
	if resolved == "" {
		return nil, nil
	}
	return []string{resolved}, nil
}

// resolveExplicitConfigFiles 校验显式文件并按标准化绝对路径拒绝重复声明。
func resolveExplicitConfigFiles(paths []string) ([]string, error) {
	result := make([]string, 0, len(paths))
	seen := make(map[string]struct{}, len(paths))
	// 每个显式路径都必须存在且是文件；任一失败会阻断整个有序文件集合。
	for index, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		resolved, err := validateConfigFile(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil, errorForConfigResolveFileFailed(messagef("op=config.builder.file result=failed stage=resolve_explicit_file file_index=%d key=%s reason=file_not_found", index, path))
			}
			return nil, err
		}
		key := filepath.Clean(resolved)
		if runtime.GOOS == "windows" {
			key = strings.ToLower(key)
		}
		if _, exists := seen[key]; exists {
			return nil, errorForConfigConfigFileInvalid(messagef("op=config.builder.file result=failed stage=resolve_explicit_file file_index=%d key=%s reason=duplicate_path", index, resolved))
		}
		seen[key] = struct{}{}
		result = append(result, resolved)
	}
	return result, nil
}

// loadBootstrapFileLayers 读取全部启动文件，并分别保留 runtime layer、顺序合并 bootstrap。
func (this *Builder) loadBootstrapFileLayers(configFiles []string) ([]runtimeFileLayer, map[string]any, error) {
	layers := make([]runtimeFileLayer, 0, len(configFiles))
	bootstrap := make(map[string]any)
	// 文件按声明顺序读取；后文件 bootstrap 与 runtime layer 都具有更高稳定覆盖顺序。
	for index, configFile := range configFiles {
		loaded, err := loadYAMLFile(configFile)
		if err != nil {
			return nil, nil, errorForConfigLoadConfigFileFailed(messagef("op=config.builder.bootstrap result=failed stage=load_config_file file_index=%d key=%s",
				index, configFile), err,
			)
		}
		fileBootstrap, err := extractBootstrapBlock(loaded)
		if err != nil {
			return nil, nil, errorForConfigBootstrapBlockInvalid(messagef("op=config.builder.bootstrap result=failed stage=parse_bootstrap_block file_index=%d key=%s",
				index, configFile), err,
			)
		}
		bootstrap = mergeMapValues(bootstrap, fileBootstrap)
		delete(loaded, BootstrapRootKey)
		layers = append(layers, runtimeFileLayer{path: configFile, data: loaded})
	}
	return layers, bootstrap, nil
}

// prepareRemoteSessions 根据 Provider 注册表打开远端会话。
// 阶段角色：在资源读取前完成 Session/Source 拓扑校验，并在后续 Provider 失败时逆向回滚。
func (this *Builder) prepareRemoteSessions(ctx context.Context, bootstrapValues map[string]any) ([]remoteSessionPlan, error) {
	names := this.sortedProviderNames()
	result := make([]remoteSessionPlan, 0, len(names))
	for _, name := range names {
		item := this.providers[name]
		session, err := callProviderOpen(item, ctx, bootstrapValues)
		if err != nil {
			openErr := errorForProviderSessionInvalid(messagef("op=config.provider result=failed stage=open_session provider=%s reason=provider_failed", name), err)
			return nil, errors.Join(openErr, closeRemoteSession(name, session, len(result)), closeRemoteSessions(result))
		}

		// nil Session 表示 Provider 在本次 bootstrap 中未启用；非 nil 会话必须形成完整 Source 拓扑。
		remoteSession, enabled, err := normalizeRemoteSession(name, session)
		if err != nil {
			return nil, errors.Join(err, closeRemoteSession(name, session, len(result)), closeRemoteSessions(result))
		}
		if !enabled {
			continue
		}
		result = append(result, remoteSession)
	}
	return result, nil
}

// closeRemoteSessions 关闭构建阶段已取得但尚未移交 Manager 的 Session。
// 阶段角色：prepareRemoteSessions 任一后续 Provider 失败时执行逆向回滚，并聚合全部关闭错误。
func closeRemoteSessions(sessions []remoteSessionPlan) error {
	var errs []error
	for index := len(sessions) - 1; index >= 0; index-- {
		if err := closeRemoteSession(sessions[index].provider, sessions[index].session, index); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// closeRemoteSession 关闭单个已取得 Session，并补充稳定的回滚上下文。
// 阶段角色：Open 返回错误、拓扑校验失败或后续 Provider 失败时执行资源补偿。
func closeRemoteSession(providerName string, session provider.Session, index int) error {
	if isNilInterface(session) {
		return nil
	}
	if err := closeManagedResource(session); err != nil {
		return errorForConfigManagerCloseResourceFailed(messagef("op=config.builder.provider result=failed stage=rollback_session provider=%s session_index=%d",
			strings.TrimSpace(providerName), index), err,
		)
	}
	return nil
}

// normalizeRemoteSession 统一校验 Provider 打开的 Session 及 Source 元信息。
// 阶段角色：在首次加载和 watcher 启动前拒绝 nil Source、空名、重名和越界优先级。
func normalizeRemoteSession(providerName string, session provider.Session) (remoteSessionPlan, bool, error) {
	providerName = strings.TrimSpace(providerName)
	if isNilInterface(session) {
		return remoteSessionPlan{}, false, nil
	}
	sources, err := callSessionSources(session)
	if err != nil {
		return remoteSessionPlan{}, false, errorForProviderSessionInvalid(messagef("op=config.provider result=failed stage=read_sources provider=%s reason=provider_failed", providerName), err)
	}
	if len(sources) == 0 {
		return remoteSessionPlan{}, false, errorForProviderSessionInvalid(messagef("op=config.provider result=failed stage=validate_session provider=%s reason=sources_required", providerName))
	}

	result := remoteSessionPlan{provider: providerName, session: session, sources: make([]remoteSource, 0, len(sources))}
	names := make(map[string]struct{}, len(sources))
	for index, source := range sources {
		if isNilInterface(source) {
			return remoteSessionPlan{}, false, errorForProviderSessionInvalid(messagef("op=config.provider result=failed stage=validate_session provider=%s source_index=%d reason=source_is_nil", providerName, index))
		}
		info, err := callSourceInfo(source)
		if err != nil {
			return remoteSessionPlan{}, false, errorForProviderSessionInvalid(messagef("op=config.provider result=failed stage=read_source_info provider=%s source_index=%d reason=provider_failed", providerName, index), err)
		}
		info.Name = strings.TrimSpace(info.Name)
		if info.Name == "" {
			return remoteSessionPlan{}, false, errorForProviderSessionInvalid(messagef("op=config.provider result=failed stage=validate_session provider=%s source_index=%d reason=source_name_required", providerName, index))
		}
		if _, exists := names[info.Name]; exists {
			return remoteSessionPlan{}, false, errorForProviderSessionInvalid(messagef("op=config.provider result=failed stage=validate_session provider=%s source=%s reason=duplicate_source_name", providerName, info.Name))
		}
		if info.Priority < remoteSourcePriorityMin || info.Priority > remoteSourcePriorityMax {
			return remoteSessionPlan{}, false, errorForProviderSessionInvalid(messagef("op=config.builder.provider result=failed stage=validate_remote_source_priority provider=%s source=%s priority=%d allowed=%d..%d reason=priority_out_of_range",
				providerName, info.Name, info.Priority, remoteSourcePriorityMin, remoteSourcePriorityMax,
			))
		}
		names[info.Name] = struct{}{}
		result.sources = append(result.sources, remoteSource{source: source, info: info})
	}
	return result, true, nil
}

// callProviderOpen 隔离 Provider.Open panic，使此前已打开的 Session 能进入统一回滚路径。
func callProviderOpen(item provider.Provider, ctx context.Context, values map[string]any) (session provider.Session, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = newProviderCallPanicError("open_session", recovered)
		}
	}()
	return item.Open(ctx, values)
}

// callSessionSources 隔离 Session.Sources panic，使当前 Session 能由调用方关闭。
func callSessionSources(session provider.Session) (sources []provider.Source, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = newProviderCallPanicError("read_sources", recovered)
		}
	}()
	return session.Sources(), nil
}

// callSourceInfo 隔离 Source.Info panic，避免拓扑校验异常跳过 Session 回滚。
func callSourceInfo(source provider.Source) (info provider.SourceInfo, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = newProviderCallPanicError("read_source_info", recovered)
		}
	}()
	return source.Info(), nil
}

// callSessionLoad 隔离 Session.Load panic；panic 必须阻断发布，不能按 Optional 降级。
func callSessionLoad(session provider.Session, ctx context.Context, source provider.Source) (content []byte, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = newProviderCallPanicError("load_source", recovered)
		}
	}()
	return session.Load(ctx, source)
}

// callSessionWatch 隔离 WatchableSession.Watch panic，使 Build 能关闭已接管的 watcher 与 Session。
func callSessionWatch(session provider.WatchableSession, ctx context.Context, source provider.Source, onChange func()) (closer io.Closer, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = newProviderCallPanicError("watch_source", recovered)
		}
	}()
	return session.Watch(ctx, source, onChange)
}

// isProviderCallPanic 判断错误是否来自 Provider SPI panic。
func isProviderCallPanic(err error) bool {
	var target *providerCallPanicError
	return errors.As(err, &target)
}

// startRemoteWatchers 为每个远端 source 启动监听。
func (this *Builder) startRemoteWatchers(ctx context.Context, manager *defaultManager, plan runtimePlan) error {
	onRefreshErr := this.onRefreshErr
	for _, remoteSession := range plan.remoteSessions {
		watchable, ok := remoteSession.session.(provider.WatchableSession)
		if !ok || isNilInterface(watchable) {
			continue
		}
		stages := stageSetForProvider(remoteSession.provider)
		for _, item := range sortedRemoteSources(remoteSession.sources) {
			watchSource := item
			watcherCloser, err := callSessionWatch(watchable, ctx, watchSource.source, func() {
				refreshErr := manager.refresh(ctx)
				if refreshErr != nil && onRefreshErr != nil {
					emitRefreshError(onRefreshErr, errorForConfigWatchRefreshFailed(messagef("op=config.builder.watch result=failed stage=%s provider=%s source=%s",
						stages.watchRefresh,
						remoteSession.provider,
						watchSource.info.Name), refreshErr,
					))
				}
			})
			if err != nil {
				if watchSource.info.Optional && !isProviderCallPanic(err) {
					continue
				}
				return errorForConfigWatchFailed(messagef("op=config.builder.watch result=failed stage=%s provider=%s source=%s",
					stages.watch,
					remoteSession.provider,
					watchSource.info.Name), err,
				)
			}
			manager.addWatcher(watcherCloser)
		}
	}
	return nil
}

type localFileWatchState struct {
	// size 是最近一次成功扫描的文件字节数。
	size int64
	// modUnixNano 是最近一次成功扫描的 UTC 修改时间戳。
	modUnixNano int64
}

// localFileWatchStates 保存一个 Manager 全部本地文件的最近成功扫描状态。
type localFileWatchStates map[string]localFileWatchState

// startLocalFileWatcher 在启用本地文件监听时启动轮询 watcher。
func (this *Builder) startLocalFileWatcher(ctx context.Context, manager *defaultManager, plan runtimePlan) error {
	// 阶段一：只有显式启用且最终存在文件集合时才取得 watcher 资源。
	if !this.enableLocalFileWatch {
		return nil
	}
	configFiles := append([]string{}, plan.configFiles...)
	if len(configFiles) == 0 {
		return nil
	}

	// 阶段二：固化整个文件集合共享的轮询周期。
	interval := plan.localFileWatchInterval
	if interval <= 0 {
		interval = defaultLocalFileWatchInterval
	}

	// 阶段三：启动前完整采样全部路径，避免部分可监听状态进入运行期。
	state, err := readLocalFileWatchStates(configFiles)
	if err != nil {
		return errorForConfigWatchFailed(messagef("op=config.builder.watch result=failed stage=watch_local_files_stat file_count=%d",
			len(configFiles)), err,
		)
	}
	go this.runLocalFileWatchLoop(ctx, manager, configFiles, interval, state)
	manager.addWatcher(nil)
	return nil
}

// runLocalFileWatchLoop 驱动本地文件轮询主循环。
//
// 执行步骤：
// 1. 按固定间隔读取文件元信息；
// 2. 元信息变化时触发 refresh；
// 3. refresh 或轮询读取失败时，通过错误回调上报但不中断循环。
func (this *Builder) runLocalFileWatchLoop(
	ctx context.Context,
	manager *defaultManager,
	configFiles []string,
	interval time.Duration,
	initial localFileWatchStates,
) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	current := initial
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			current = this.tryRefreshOnLocalFileChange(ctx, manager, configFiles, current, "watch_local_files_poll_scan", "watch_local_files_poll_refresh")
		}
	}
}

// tryRefreshOnLocalFileChange 负责在“检测到文件状态变化”后触发一次 refresh 发布流程。
//
// 执行步骤：
// 1. 先读取最新文件元信息；
// 2. 若与当前状态一致则跳过 refresh；
// 3. 若状态变化则触发 manager.refresh，并按阶段上报错误。
func (this *Builder) tryRefreshOnLocalFileChange(
	ctx context.Context,
	manager *defaultManager,
	configFiles []string,
	current localFileWatchStates,
	scanStage string,
	refreshStage string,
) localFileWatchStates {
	// 阶段一：一次性采样全部文件；任一 stat 失败时保留上一份完整状态。
	latest, watchErr := readLocalFileWatchStates(configFiles)
	if watchErr != nil {
		if this.onRefreshErr != nil {
			emitRefreshError(this.onRefreshErr, errorForConfigWatchRefreshFailed(messagef("op=config.builder.watch result=failed stage=%s file_count=%d",
				scanStage, len(configFiles)), watchErr,
			))
		}
		return current
	}
	// 阶段二：只有完整集合发生变化才触发全量 refresh。
	if localFileWatchStatesEqual(latest, current) {
		return current
	}

	// 阶段三：发布成功后才确认扫描基线；发布前失败时保留差异，供下一轮重试。
	published, refreshErr := manager.refreshWithPublishStatus(ctx)
	if published {
		current = latest
	}
	if refreshErr != nil && this.onRefreshErr != nil {
		emitRefreshError(this.onRefreshErr, errorForConfigWatchRefreshFailed(messagef("op=config.builder.watch result=failed stage=%s file_count=%d",
			refreshStage, len(configFiles)), refreshErr,
		))
	}
	return current
}

// readLocalFileWatchStates 读取全部文件的变更检测元信息。
// 阶段角色：启动 watcher 和每轮扫描共用的完整状态采样，任一文件失败则整轮失败。
func readLocalFileWatchStates(paths []string) (localFileWatchStates, error) {
	result := make(localFileWatchStates, len(paths))
	for _, path := range paths {
		state, err := readLocalFileWatchState(path)
		if err != nil {
			return nil, errorf("op=config.builder.watch result=failed stage=stat_file key=%s reason=%w", path, err)
		}
		result[path] = state
	}
	return result, nil
}

// localFileWatchStatesEqual 判断两个完整文件集合状态是否一致。
func localFileWatchStatesEqual(left, right localFileWatchStates) bool {
	if len(left) != len(right) {
		return false
	}
	for path, state := range left {
		if other, exists := right[path]; !exists || other != state {
			return false
		}
	}
	return true
}

// readLocalFileWatchState 读取用于文件变更检测的元信息快照。
func readLocalFileWatchState(path string) (localFileWatchState, error) {
	info, err := os.Stat(path)
	if err != nil {
		return localFileWatchState{}, err
	}
	return localFileWatchState{
		size:        info.Size(),
		modUnixNano: info.ModTime().UTC().UnixNano(),
	}, nil
}

// emitRefreshError 调用错误上报回调，并隔离回调 panic。
func emitRefreshError(handler func(error), err error) {
	if handler == nil || err == nil {
		return
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			log.Printf("op=config.builder.refresh_error_handler result=failed stage=callback_panic reason=%v", recovered)
		}
	}()
	handler(err)
}

// buildSnapshot 执行一次完整的配置重算。
func (this *Builder) buildSnapshot(ctx context.Context, plan runtimePlan) (snapshotState, error) {
	layers := make([]mergeLayer, 0, len(plan.fileLayers)+4)

	// 阶段一：准备最低优先级进程默认层。
	if this.defaults != nil {
		defaultLayer, err := toMapFromAny(this.defaults)
		if err != nil {
			return snapshotState{}, errorForConfigSnapshotBuildDefaultFailed(messagef("op=config.builder.snapshot result=failed stage=build_default_layer source=default"), err)
		}
		layers = append(layers, mergeLayer{Name: string(SourceKindDefault), Source: SourceKindDefault, Priority: defaultLayerPriority, Data: defaultLayer})
	}

	// 阶段二：启用 file watch 时重读全部文件，否则复用启动阶段缓存层。
	fileLayers := plan.fileLayers
	if this.enableLocalFileWatch && len(plan.configFiles) > 0 {
		latestFileLayers, err := this.loadRuntimeFileLayers(plan.configFiles)
		if err != nil {
			return snapshotState{}, err
		}
		fileLayers = latestFileLayers
	}
	// 每个文件保持独立 layer，同优先级下按声明顺序稳定覆盖并保留 Explain 来源。
	for _, fileLayer := range fileLayers {
		if len(fileLayer.data) == 0 {
			continue
		}
		layers = append(layers, mergeLayer{Name: fileLayerName(fileLayer.path), Source: SourceKindFile, Priority: fileLayerPriority, Data: deepCopyMap(fileLayer.data)})
	}

	// 阶段三：按稳定 Provider/Source 顺序加载远端层。
	for _, remoteSession := range plan.remoteSessions {
		stages := stageSetForProvider(remoteSession.provider)
		for _, item := range sortedRemoteSources(remoteSession.sources) {
			content, err := callSessionLoad(remoteSession.session, ctx, item.source)
			if err != nil {
				if item.info.Optional && !isProviderCallPanic(err) {
					continue
				}
				return snapshotState{}, errorForConfigSnapshotRemoteLoadFailed(messagef("op=config.builder.snapshot result=failed stage=%s provider=%s source=%s",
					stages.loadLayer,
					remoteSession.provider,
					item.info.Name), err,
				)
			}
			remoteLayer, err := parseYAMLBytes(content)
			if err != nil {
				return snapshotState{}, errorForConfigSnapshotRemoteParseFailed(messagef("op=config.builder.snapshot result=failed stage=%s provider=%s source=%s",
					stages.parseLayer,
					remoteSession.provider,
					item.info.Name), err,
				)
			}
			layers = append(layers, mergeLayer{
				Name:     remoteSession.provider + ":" + item.info.Name,
				Source:   SourceKindRemote,
				Priority: remoteLayerPriorityBase + item.info.Priority,
				Data:     remoteLayer,
			})
		}
	}

	// 阶段四：追加最终 pinned 强覆盖层。
	if len(plan.pinnedValues) > 0 {
		layers = append(layers, mergeLayer{Name: string(SourceKindPinned), Source: SourceKindPinned, Priority: pinnedLayerPriority, Data: deepCopyMap(plan.pinnedValues), Pinned: true})
	}

	// 阶段五：统一合并全部层并生成可解释元数据。
	merged, mergedMeta := combineLayers(layers...)
	meta := make(map[string]ValueMeta, len(mergedMeta))
	for key, item := range mergedMeta {
		meta[key] = ValueMeta{
			Source:   item.Source,
			Layer:    item.Layer,
			Priority: item.Priority,
			Pinned:   item.Pinned,
		}
	}

	raw := deepCopyMap(merged)
	flat := flattenMap(raw)

	return snapshotState{
		raw:       raw,
		flat:      flat,
		meta:      meta,
		updatedAt: time.Now(),
	}, nil
}

// loadRuntimeFileLayers 重读全部配置文件并转换为有序 runtime layers。
func (this *Builder) loadRuntimeFileLayers(configFiles []string) ([]runtimeFileLayer, error) {
	result := make([]runtimeFileLayer, 0, len(configFiles))
	for index, configFile := range configFiles {
		loaded, err := loadYAMLFile(configFile)
		if err != nil {
			return nil, errorForConfigLoadConfigFileFailed(messagef("op=config.builder.snapshot result=failed stage=load_runtime_file_layer file_index=%d key=%s",
				index, configFile), err,
			)
		}
		delete(loaded, BootstrapRootKey)
		result = append(result, runtimeFileLayer{path: configFile, data: loaded})
	}
	return result, nil
}

// fileLayerName 返回 Explain 使用的稳定文件 layer 名称。
func fileLayerName(path string) string {
	return string(SourceKindFile) + ":" + filepath.ToSlash(filepath.Clean(path))
}

// sortedRemoteSources 返回按优先级升序排列后的 source 列表。
func sortedRemoteSources(input []remoteSource) []remoteSource {
	result := make([]remoteSource, 0, len(input))
	result = append(result, input...)
	sort.SliceStable(result, func(i, j int) bool {
		return result[i].info.Priority < result[j].info.Priority
	})
	return result
}

// stageSetForProvider 返回 provider 对应的错误 stage 命名集合。
func stageSetForProvider(providerName string) remoteStageSet {
	token := stageToken(providerName)
	if token == "" {
		token = "remote"
	}
	return remoteStageSet{
		watch:        "watch_" + token,
		watchRefresh: "watch_" + token + "_refresh",
		loadLayer:    "load_" + token + "_layer",
		parseLayer:   "parse_" + token + "_layer",
	}
}

// stageToken 把 provider 名称转换为可读稳定的 stage 片段。
func stageToken(providerName string) string {
	providerName = strings.TrimSpace(strings.ToLower(providerName))
	if providerName == "" {
		return ""
	}
	var builder strings.Builder
	for _, current := range providerName {
		isAlphaNum := (current >= 'a' && current <= 'z') || (current >= '0' && current <= '9')
		if isAlphaNum {
			builder.WriteRune(current)
			continue
		}
		if builder.Len() > 0 {
			builder.WriteRune('_')
		}
	}
	token := strings.Trim(builder.String(), "_")
	return token
}

// resolveConfigFilePath 解析显式配置路径，或回退到默认文件列表。
func resolveConfigFilePath(explicitPath string) (string, error) {
	explicitPath = strings.TrimSpace(explicitPath)
	if explicitPath != "" {
		resolved, err := validateConfigFile(explicitPath)
		if err == nil {
			return resolved, nil
		}
		if errors.Is(err, os.ErrNotExist) {
			return "", errorForConfigResolveFileFailed(messagef("config file not found path=" + explicitPath))
		}
		return "", err
	}

	for _, candidate := range defaultConfigFiles {
		resolved, err := validateConfigFile(candidate)
		if err == nil {
			return resolved, nil
		}
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		return "", err
	}

	return "", nil
}

// validateConfigFile 校验配置路径，并返回清洗后的绝对路径。
func validateConfigFile(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", os.ErrNotExist
		}
		return "", errorForConfigResolveFileFailed(messagef("config path stat failed path=%s", path), err)
	}
	if info.IsDir() {
		return "", errorForConfigConfigFileInvalid(messagef("config path " + path + " is a directory"))
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return path, nil
	}
	return absPath, nil
}

// extractBootstrapBlock 从完整配置内容中提取 bootstrap 块。
func extractBootstrapBlock(fileLayer map[string]any) (map[string]any, error) {
	if fileLayer == nil {
		return map[string]any{}, nil
	}
	raw, ok := fileLayer[BootstrapRootKey]
	if !ok || raw == nil {
		return map[string]any{}, nil
	}
	value := normalizeValue(raw)
	block, ok := value.(map[string]any)
	if !ok {
		return nil, errorForConfigBootstrapBlockInvalid(messagef("bootstrap block must be a map"))
	}
	return normalizeInputValues(block, false), nil
}

// normalizeInputValues 归一化输入 map，支持 dotted key 与 bootstrap 根块展开。
func normalizeInputValues(values map[string]any, unwrapBootstrapRoot bool) map[string]any {
	result := make(map[string]any)
	if len(values) == 0 {
		return result
	}
	for key, value := range values {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		normalized := normalizeValue(value)
		if unwrapBootstrapRoot && key == BootstrapRootKey {
			if block, ok := normalized.(map[string]any); ok {
				result = mergeMapValues(result, normalizeInputValues(block, false))
			}
			continue
		}
		if strings.Contains(key, ".") {
			setByDottedKey(result, key, normalized)
			continue
		}
		if block, ok := normalized.(map[string]any); ok {
			if existing, exists := result[key].(map[string]any); exists {
				result[key] = mergeMapValues(existing, block)
				continue
			}
			result[key] = deepCopyMap(block)
			continue
		}
		result[key] = normalized
	}
	return result
}

// mergeMapValues 深度合并 map，overlay 同名字段覆盖 base。
func mergeMapValues(base map[string]any, overlay map[string]any) map[string]any {
	if len(base) == 0 && len(overlay) == 0 {
		return map[string]any{}
	}
	result := make(map[string]any, len(base)+len(overlay))
	for key, value := range base {
		result[key] = deepCopyValue(value)
	}
	for key, value := range overlay {
		normalized := normalizeValue(value)
		overlayMap, overlayIsMap := normalized.(map[string]any)
		existingMap, baseIsMap := result[key].(map[string]any)
		if overlayIsMap && baseIsMap {
			result[key] = mergeMapValues(existingMap, overlayMap)
			continue
		}
		result[key] = deepCopyValue(normalized)
	}
	return result
}

// loadBootstrapEnvValues 把环境变量映射为启动覆盖值。
func (this *Builder) loadBootstrapEnvValues() map[string]any {
	result := provider.LoadBootstrapFromEnvBySchema(this.bootstrapEnvPrefix, CoreBootstrapSchema())
	prefix := normalizeEnvPrefix(this.bootstrapEnvPrefix)
	for _, name := range this.sortedProviderNames() {
		item := this.providers[name]
		providerValues := map[string]any{}
		if schemaProvider, ok := item.(provider.BootstrapSchemaProvider); ok {
			providerValues = provider.LoadBootstrapFromEnvBySchema(prefix, schemaProvider.BootstrapSchema())
		}
		if loader, ok := item.(provider.BootstrapEnvLoader); ok {
			extended := normalizeInputValues(loader.LoadBootstrapFromEnv(prefix), false)
			providerValues = mergeMapValues(providerValues, extended)
		}
		result = mergeMapValues(result, providerValues)
	}
	return result
}

// sortedProviderNames 返回 provider 名称的稳定有序视图。
func (this *Builder) sortedProviderNames() []string {
	result := make([]string, 0, len(this.providers))
	for name := range this.providers {
		result = append(result, name)
	}
	sort.Strings(result)
	return result
}

// normalizeEnvPrefix 把前缀统一转换为大写下划线形式。
func normalizeEnvPrefix(prefix string) string {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return ""
	}
	prefix = strings.ReplaceAll(prefix, "-", "_")
	prefix = strings.ReplaceAll(prefix, ".", "_")
	prefix = strings.ToUpper(prefix)
	if strings.HasSuffix(prefix, "_") {
		return prefix
	}
	return prefix + "_"
}

// resolveLocalFileWatchInterval 解析本地文件监听轮询间隔。
//
// 优先级：
// 1. File(FileWatch(interval...)) 的显式参数（>0）；
// 2. 配置文件 bootstrap.configFile.watchInterval（仅文件来源）；
// 3. 默认值 30s。
func (this *Builder) resolveLocalFileWatchInterval(fileBootstrap map[string]any) time.Duration {
	if this.localFileWatchExplicit && this.localFileWatchInterval > 0 {
		return this.localFileWatchInterval
	}
	interval, ok := readBootstrapConfigFileWatchInterval(fileBootstrap)
	if ok {
		return interval
	}
	return defaultLocalFileWatchInterval
}

// stripBootstrapConfigFileWatchBlock 移除本地文件监听 bootstrap 子块，避免进入 provider/bootstrap 合并路径。
func stripBootstrapConfigFileWatchBlock(values map[string]any) map[string]any {
	if len(values) == 0 {
		return map[string]any{}
	}
	result := deepCopyMap(values)
	delete(result, BootstrapKeyConfigFileWatch)
	return result
}

// readBootstrapConfigFileWatchInterval 读取并校验文件内轮询间隔。
//
// 返回：
// - interval: 解析成功且满足范围约束后的轮询间隔；
// - ok: false 表示缺失或非法，调用方应回退默认值。
func readBootstrapConfigFileWatchInterval(values map[string]any) (interval time.Duration, ok bool) {
	raw, exists := lookupByDottedKey(values, BootstrapKeyConfigFileWatchInterval)
	if !exists || raw == nil {
		return 0, false
	}
	parsed, valid := parseConfigFileWatchInterval(raw)
	if !valid {
		return 0, false
	}
	if !isConfigFileWatchIntervalInRange(parsed) {
		return 0, false
	}
	return parsed, true
}

// parseConfigFileWatchInterval 把 YAML 反序列化值转换为轮询间隔。
//
// 语义：
// 1. string 支持 ms/s/min 三种常用单位；
// 2. string 无单位时按“秒”解析；
// 3. number（int/uint/float）按“秒”解析。
func parseConfigFileWatchInterval(raw any) (time.Duration, bool) {
	switch typed := raw.(type) {
	case string:
		return parseConfigFileWatchIntervalString(typed)
	case int:
		if typed <= 0 {
			return 0, false
		}
		return time.Duration(typed) * time.Second, true
	case int8:
		if typed <= 0 {
			return 0, false
		}
		return time.Duration(typed) * time.Second, true
	case int16:
		if typed <= 0 {
			return 0, false
		}
		return time.Duration(typed) * time.Second, true
	case int32:
		if typed <= 0 {
			return 0, false
		}
		return time.Duration(typed) * time.Second, true
	case int64:
		if typed <= 0 {
			return 0, false
		}
		return time.Duration(typed) * time.Second, true
	case uint:
		if typed == 0 || uint64(typed) > math.MaxInt64 {
			return 0, false
		}
		return time.Duration(typed) * time.Second, true
	case uint8:
		if typed == 0 {
			return 0, false
		}
		return time.Duration(typed) * time.Second, true
	case uint16:
		if typed == 0 {
			return 0, false
		}
		return time.Duration(typed) * time.Second, true
	case uint32:
		if typed == 0 {
			return 0, false
		}
		return time.Duration(typed) * time.Second, true
	case uint64:
		if typed == 0 || typed > math.MaxInt64 {
			return 0, false
		}
		return time.Duration(typed) * time.Second, true
	case float32:
		return parseConfigFileWatchInterval(float64(typed))
	case float64:
		if typed <= 0 || typed > float64(math.MaxInt64/time.Second) {
			return 0, false
		}
		return time.Duration(typed * float64(time.Second)), true
	default:
		return 0, false
	}
}

// parseConfigFileWatchIntervalString 解析字符串形式的轮询间隔。
//
// 可接受示例：
// 1. 250ms / 5s / 3min
// 2. 15（无单位，按秒）
func parseConfigFileWatchIntervalString(raw string) (time.Duration, bool) {
	text := strings.TrimSpace(strings.ToLower(raw))
	if text == "" {
		return 0, false
	}
	// min 非 Go duration 原生单位，这里做兼容映射。
	if strings.HasSuffix(text, "min") {
		text = strings.TrimSpace(strings.TrimSuffix(text, "min")) + "m"
	}
	// 无单位时按“秒”解释。
	if !strings.HasSuffix(text, "ms") && !strings.HasSuffix(text, "s") && !strings.HasSuffix(text, "m") {
		text = text + "s"
	}
	interval, err := time.ParseDuration(text)
	if err != nil || interval <= 0 {
		return 0, false
	}
	return interval, true
}

// isConfigFileWatchIntervalInRange 校验轮询间隔是否位于 [1s,60min] 区间。
func isConfigFileWatchIntervalInRange(interval time.Duration) bool {
	return interval >= localFileWatchIntervalMin && interval <= localFileWatchIntervalMax
}

// readStringByDottedKey 读取 dotted key 对应的字符串值。
func readBootstrapConfigPath(values map[string]any) (string, error) {
	raw, ok := lookupByDottedKey(values, BootstrapKeyConfigFile)
	if !ok || raw == nil {
		return "", nil
	}
	value, castOK := raw.(string)
	if !castOK {
		return "", errorForBootstrapInvalidType(messagef(fmt.Sprintf("invalid bootstrap key %s type %T, expect string", BootstrapKeyConfigFile, raw)))
	}
	return strings.TrimSpace(value), nil
}

// validateBootstrapValues 对 bootstrap 合并结果执行 strict 校验（未知 key 直接失败）。
func (this *Builder) validateBootstrapValues(values map[string]any) error {
	allowedRoots := make(map[string]struct{}, len(this.providers)+1)
	allowedRoots[BootstrapKeyConfigFile] = struct{}{}
	for _, name := range this.sortedProviderNames() {
		if _, ok := this.providers[name].(provider.BootstrapSchemaProvider); ok {
			allowedRoots[name] = struct{}{}
		}
	}

	rootNames := make([]string, 0, len(values))
	for key := range values {
		rootNames = append(rootNames, key)
	}
	sort.Strings(rootNames)
	allowedRootList := make([]string, 0, len(allowedRoots))
	for key := range allowedRoots {
		allowedRootList = append(allowedRootList, key)
	}
	sort.Strings(allowedRootList)

	for _, key := range rootNames {
		if _, ok := allowedRoots[key]; ok {
			continue
		}
		hint := closestKeyHint(key, allowedRootList)
		if hint == "" {
			return errorForBootstrapUnknownKey(messagef(fmt.Sprintf("unknown bootstrap key %s", key)))
		}
		return errorForBootstrapUnknownKey(messagef(fmt.Sprintf("unknown bootstrap key %s (did_you_mean=%s)", key, hint)))
	}

	if raw, exists := values[BootstrapKeyConfigFile]; exists {
		switch raw.(type) {
		case string:
			// valid
		default:
			return errorForBootstrapInvalidType(messagef(fmt.Sprintf("invalid bootstrap key %s type %T, expect string", BootstrapKeyConfigFile, raw)))
		}
	}

	for _, name := range this.sortedProviderNames() {
		validator, ok := this.providers[name].(provider.BootstrapValidator)
		if !ok {
			continue
		}
		if err := validator.ValidateBootstrap(values); err != nil {
			return errorForBootstrapProviderInvalid(messagef("provider bootstrap is invalid provider=%s",
				name), err,
			)
		}
	}
	return nil
}

// closestKeyHint 返回最接近输入 key 的候选提示。
func closestKeyHint(input string, candidates []string) string {
	input = strings.TrimSpace(input)
	if input == "" || len(candidates) == 0 {
		return ""
	}
	best := ""
	bestDistance := 1<<31 - 1
	for _, candidate := range candidates {
		distance := editDistance(input, candidate)
		if distance < bestDistance {
			bestDistance = distance
			best = candidate
		}
	}
	if bestDistance > 3 {
		return ""
	}
	return best
}

// editDistance 计算两个字符串的 Levenshtein 距离。
func editDistance(left string, right string) int {
	leftRunes := []rune(left)
	rightRunes := []rune(right)
	leftLen := len(leftRunes)
	rightLen := len(rightRunes)
	if leftLen == 0 {
		return rightLen
	}
	if rightLen == 0 {
		return leftLen
	}

	column := make([]int, rightLen+1)
	for index := 0; index <= rightLen; index++ {
		column[index] = index
	}
	for i := 1; i <= leftLen; i++ {
		prevDiag := column[0]
		column[0] = i
		for j := 1; j <= rightLen; j++ {
			temp := column[j]
			cost := 0
			if leftRunes[i-1] != rightRunes[j-1] {
				cost = 1
			}
			insert := column[j] + 1
			delete := column[j-1] + 1
			replace := prevDiag + cost
			column[j] = minInt(insert, minInt(delete, replace))
			prevDiag = temp
		}
	}
	return column[rightLen]
}

func minInt(left int, right int) int {
	if left < right {
		return left
	}
	return right
}
