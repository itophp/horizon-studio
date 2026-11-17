package studio

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// cliBinary returns the horizon-cli used to create new projects. A deployment
// can override it with STUDIO_CLI; managed projects always build their own CLI
// into their project root, and that per-project binary is what catalogue and
// service commands actually run.
func cliBinary() string {
	if path := strings.TrimSpace(os.Getenv("STUDIO_CLI")); path != "" {
		return path
	}
	if cwd, err := os.Getwd(); err == nil {
		for _, dir := range []string{cwd, filepath.Dir(cwd)} {
			binary := filepath.Join(dir, "resources", "horizon-cli", "horizon-cli")
			if info, err := os.Stat(binary); err == nil && info.Mode().IsRegular() && info.Mode()&0o111 != 0 {
				return binary
			}
		}
	}
	return "horizon-cli"
}

// Managed checkouts use their own environment and development ports.
func projectEnvironmentVariables(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		key, _, _ := strings.Cut(value, "=")
		switch strings.ToUpper(key) {
		case "DATABASE_URL", "REDIS_URL", "API_ADDR", "WEB_PORT", "API_PROXY_TARGET":
			continue
		}
		result = append(result, value)
	}
	return result
}

func run(ctx context.Context, binary string, args []string, dir string, timeout time.Duration) (string, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Dir = dir
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.Env = append(projectEnvironmentVariables(os.Environ()), "GIT_TERMINAL_PROMPT=0")
	err := cmd.Run()
	if ctx.Err() != nil {
		return "", false, errors.New("进程执行超时")
	}
	if err == nil {
		return stdout.String(), false, nil
	}
	if stdout.Len() > 0 {
		return stdout.String(), true, nil
	}
	if _, lookErr := exec.LookPath(binary); lookErr != nil {
		return "", false, errors.New("找不到 horizon-cli，请先在项目根目录构建 CLI")
	}
	return "", false, fmt.Errorf("进程执行失败：%s", strings.TrimSpace(stderr.String()))
}

func runJSON(ctx context.Context, project projectRef, args []string) (json.RawMessage, bool, error) {
	root, binary, err := projectCLI(project.Root)
	if err != nil {
		return nil, false, err
	}
	stdout, failed, err := run(ctx, binary, args, root, 90*time.Second)
	if err != nil {
		return nil, false, err
	}
	var value json.RawMessage
	if json.Unmarshal([]byte(stdout), &value) != nil {
		return nil, false, errors.New("项目 CLI 未返回有效 JSON")
	}
	return value, failed, nil
}

// projectRef is the persisted projection of a managed project row.
type projectRef struct {
	ID   int64
	Root string
}

// projectRoot validates that the directory is a Horizon project root and
// returns its canonical path, so symlink aliases resolve to one record.
func projectRoot(path string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", errors.New("请输入绝对路径")
	}
	root, err := filepath.EvalSymlinks(filepath.Clean(path))
	if err != nil {
		return "", errors.New("请选择存在的 Horizon 项目根目录")
	}
	if info, err := os.Stat(filepath.Join(root, "go.mod")); err != nil || !info.Mode().IsRegular() {
		return "", errors.New("请选择 Horizon 项目根目录")
	}
	for _, entry := range []string{"cmd/api/main.go", "foundation/cmd/api/main.go"} {
		if info, err := os.Stat(filepath.Join(root, filepath.FromSlash(entry))); err == nil && info.Mode().IsRegular() {
			return root, nil
		}
	}
	return "", errors.New("请选择 Horizon 项目根目录")
}

// projectCLI confirms the root is unchanged and its own horizon-cli binary is
// built at the project root, matching the local development convention.
func projectCLI(root string) (string, string, error) {
	canon, err := projectRoot(root)
	if err != nil {
		return "", "", err
	}
	binary := filepath.Join(canon, "horizon-cli")
	info, err := os.Stat(binary)
	if err != nil || info.IsDir() {
		return "", "", errors.New("项目已关联。请按目标项目的本地开发文档，在项目根目录构建 horizon-cli")
	}
	if info.Mode()&0o111 == 0 {
		return "", "", errors.New("项目 CLI 不可执行，请重新构建 horizon-cli")
	}
	return canon, binary, nil
}

// initializeProject creates a fresh Horizon project via the workspace CLI init.
func initializeProject(ctx context.Context, target, repository string) (string, error) {
	if !filepath.IsAbs(target) {
		return "", errors.New("请输入绝对路径")
	}
	target = filepath.Clean(target)
	parent := filepath.Clean(filepath.Dir(target))
	if info, err := os.Stat(parent); err != nil || !info.IsDir() {
		return "", errors.New("上级目录不存在，请先选择一个已有的上级目录")
	}
	if _, err := os.Lstat(target); err == nil {
		return "", errors.New("目标目录已经存在。创建项目请填写新目录，打开已有项目请选「关联已有项目」")
	}
	args := []string{"init"}
	if source := strings.TrimSpace(repository); source != "" {
		args = append(args, "--repo", source)
	}
	if _, failed, err := run(ctx, cliBinary(), append(args, target), parent, 300*time.Second); err != nil || failed {
		if err != nil {
			return "", err
		}
		return "", errors.New("Horizon CLI 无法初始化项目。请检查目标目录权限和 Git 来源")
	}
	root, err := projectRoot(target)
	if err != nil {
		return "", errors.New("仓库已克隆，但其中缺少 Horizon 项目文件。请检查 Git 来源")
	}
	return root, nil
}

// loopbackOrigin accepts only plain loopback http(s) origins, matching the
// project API_ADDR / WEB_PORT contract.
func loopbackOrigin(value string) (string, error) {
	parsed, err := url.Parse(value)
	if err != nil {
		return "", errors.New("请填写本机服务地址")
	}
	host := parsed.Hostname()
	port, portErr := strconv.Atoi(parsed.Port())
	if portErr != nil || port < 1 || port > 65535 {
		return "", errors.New("无效的本机服务端口")
	}
	if parsed.Opaque != "" || (parsed.Scheme != "http" && parsed.Scheme != "https") ||
		(host != "localhost" && !net.ParseIP(host).IsLoopback()) ||
		parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" ||
		(parsed.Path != "" && parsed.Path != "/") {
		return "", errors.New("请填写本机服务地址")
	}
	return parsed.Scheme + "://" + parsed.Host, nil
}

var controlActions = map[string]bool{"start": true, "stop": true, "restart": true, "status": true}
var controlTargets = map[string]bool{"all": true, "backend": true, "frontend": true}
var catalogKinds = map[string]bool{"plugin": true, "application": true, "capability": true, "workflow": true, "graph": true}
var logServices = map[string]bool{"backend": true, "frontend": true}

// control starts, stops, restarts or queries the project dev services through
// the project CLI.
func control(ctx context.Context, project projectRef, action, target string) (json.RawMessage, error) {
	if !controlActions[action] || !controlTargets[target] {
		return nil, errors.New("无效的进程操作")
	}
	stdout, failed, err := runJSON(ctx, project, []string{action, target, "--json"})
	if err != nil {
		return nil, err
	}
	if failed {
		return nil, errors.New("项目 CLI 报告服务操作失败")
	}
	return stdout, nil
}

func environment(ctx context.Context, project projectRef, target string) (json.RawMessage, error) {
	if !controlTargets[target] {
		return nil, errors.New("未知的服务")
	}
	stdout, _, err := runJSON(ctx, project, []string{"env", "check", target})
	return stdout, err
}

// inspect reads the project definition catalogue: the whole kind, or one fixed
// version's schema.
func inspect(ctx context.Context, project projectRef, kind, reference string) (json.RawMessage, error) {
	if !catalogKinds[kind] {
		return nil, errors.New("未知的目录类型")
	}
	args := []string{kind, "--json"}
	if reference != "" {
		if !fixedReference(reference) {
			return nil, errors.New("请选择固定版本")
		}
		args = []string{kind, "schema", reference}
	}
	stdout, failed, err := runJSON(ctx, project, args)
	if err != nil {
		return nil, err
	}
	if failed {
		return nil, errors.New("CLI 目录查询失败")
	}
	return stdout, nil
}

func fixedReference(reference string) bool {
	at := strings.LastIndex(reference, "@")
	if at <= 0 || at == len(reference)-1 {
		return false
	}
	for _, r := range reference {
		if !strings.ContainsRune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789._:-@", r) {
			return false
		}
	}
	return true
}

// readLog tails the project CLI service log, pinned inside the project log
// directory so a symlinked name cannot escape it.
func readLog(project projectRef, service string) (string, error) {
	if !logServices[service] {
		return "", errors.New("未知的服务")
	}
	root, err := projectRoot(project.Root)
	if err != nil {
		return "", err
	}
	dir, err := os.OpenRoot(root)
	if err != nil {
		return "", errors.New("项目日志目录不可读")
	}
	defer dir.Close()
	path := filepath.Join(".local", "horizon-cli", service+".log")
	info, err := dir.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil || !info.Mode().IsRegular() {
		return "", errors.New("日志文件不可读")
	}
	file, err := dir.Open(path)
	if err != nil {
		return "", errors.New("日志文件不可读")
	}
	defer file.Close()
	current, err := file.Stat()
	if err != nil || !os.SameFile(info, current) {
		return "", errors.New("日志文件不可读")
	}
	size := min(current.Size(), 24_000)
	data := make([]byte, size)
	read, err := file.ReadAt(data, current.Size()-size)
	if err != nil && !errors.Is(err, io.EOF) {
		return "", errors.New("日志文件不可读")
	}
	return string(data[:read]), nil
}

// sourceRevision captures the managed project's git revision and dirty flag so
// acceptance evidence binds to a concrete source state.
func sourceRevision(ctx context.Context, root string) (revision string, modified bool, err error) {
	canon, err := projectRoot(root)
	if err != nil {
		return "", false, err
	}
	stdout, failed, runErr := run(ctx, "git", []string{"rev-parse", "--verify", "HEAD"}, canon, 30*time.Second)
	if runErr != nil || failed {
		return "", false, errors.New("无法确定项目源码修订")
	}
	revision = strings.TrimSpace(stdout)
	if len(revision) < 40 {
		return "", false, errors.New("无法确定项目源码修订")
	}
	status, failed, runErr := run(ctx, "git", []string{"status", "--porcelain=v1"}, canon, 30*time.Second)
	if runErr != nil || failed {
		return "", false, errors.New("无法确定项目工作树状态")
	}
	return revision, strings.TrimSpace(status) != "", nil
}
