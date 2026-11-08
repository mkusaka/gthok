#!/usr/bin/env ruby
# Offline contract checks for the Makefile, release assets, and composite action.
require "digest"
require "fileutils"
require "minitest/autorun"
require "open3"
require "tmpdir"
require "yaml"

class PackagingTest < Minitest::Test
  ROOT = File.expand_path("..", __dir__)
  ACTION = YAML.load_file(File.join(ROOT, "action.yml"))
  RELEASE = YAML.load_file(File.join(ROOT, ".github/workflows/release.yml"))

  def setup
    FileUtils.mkdir_p(File.join(ROOT, "dist"))
    @dir = Dir.mktmpdir("packaging-test-", File.join(ROOT, "dist"))
    @bin = File.join(@dir, "tools")
    @assets = File.join(@dir, "assets")
    FileUtils.mkdir_p([@bin, @assets])
    @env = {
      "PATH" => "#{@bin}:#{ENV.fetch('PATH')}", "HOME" => @dir,
      "GIT_CONFIG_NOSYSTEM" => "1", "GIT_CONFIG_GLOBAL" => "/dev/null",
      "RUNNER_TEMP" => @dir, "GITHUB_OUTPUT" => File.join(@dir, "output"),
      "GITHUB_PATH" => File.join(@dir, "path"), "GITHUB_ENV" => File.join(@dir, "env"),
      "FAKE_RELEASE" => @assets, "TOOL_LOG" => File.join(@dir, "tools.log"),
      "REPO" => "mkusaka/gthok", "TAG" => "v0.0.1"
    }
    tool("gh", <<~'SH')
      printf '%s\n' "$*" >> "$TOOL_LOG"
      case "$1 $2" in
        'release view') echo "${FAKE_TAG:-v0.0.1}" ;;
        'release verify'|'release verify-asset') ;;
        'release download')
          shift 2
          patterns=()
          while [ "$#" -gt 0 ]; do
            case "$1" in
              --pattern) patterns+=("$2"); shift 2 ;;
              --dir) dir="$2"; shift 2 ;;
              --repo) shift 2 ;;
              *) shift ;;
            esac
          done
          for asset in "${patterns[@]}"; do cp "$FAKE_RELEASE/$asset" "$dir/$asset"; done
          ;;
        *) exit 1 ;;
      esac
    SH
  end

  def teardown
    FileUtils.remove_entry(@dir)
  end

  def tool(name, body)
    path = File.join(@bin, name)
    File.write(path, "#!/usr/bin/env bash\nset -euo pipefail\n#{body}")
    FileUtils.chmod(0755, path)
  end

  def run_script(script, env = {}, cwd = ROOT)
    Open3.capture3(@env.merge(env), "bash", "-c", script, chdir: cwd)
  end

  def assert_ok(result)
    stdout, stderr, status = result
    assert status.success?, "#{stdout}\n#{stderr}"
    stdout
  end

  def step(id)
    ACTION.fetch("runs").fetch("steps").find { |s| s["id"] == id }.fetch("run")
  end

  def assets
    binary = File.join(@dir, "gthok")
    File.write(binary, <<~'SH')
      #!/usr/bin/env bash
      set -euo pipefail
      case "$1" in
        version) echo 'gthok version 0.0.1' ;;
        shim) mkdir -p "$4"; ln -s "$0" "$4/git" ;;
        *) exit 1 ;;
      esac
    SH
    FileUtils.chmod(0755, binary)
    checksums = %w[linux darwin].product(%w[amd64 arm64]).map do |os, arch|
      name = "gthok_0.0.1_#{os}_#{arch}.tar.gz"
      path = File.join(@assets, name)
      assert_ok(run_script('tar -czf "$ASSET" -C "$RUNNER_TEMP" gthok', "ASSET" => path))
      "#{Digest::SHA256.file(path).hexdigest}  #{name}\n"
    end
    File.write(File.join(@assets, "checksums.txt"), checksums.join)
  end

  def test_resolves_latest_and_explicit_versions_and_rejects_output_injection
    assert_equal "latest", ACTION.fetch("inputs").fetch("version").fetch("default")
    ["latest", "", "v0.0.1"].each do |version|
      FileUtils.rm_f(@env.fetch("GITHUB_OUTPUT"))
      assert_ok(run_script(step("resolve"), "INPUT_VERSION" => version))
      assert_equal "repo=mkusaka/gthok\ntag=v0.0.1\n", File.read(@env.fetch("GITHUB_OUTPUT"))
    end
    ["../bad", "v0.0.1\nother=value"].each do |version|
      refute run_script(step("resolve"), "INPUT_VERSION" => version).last.success?
    end
  end

  def test_installs_all_platform_assets_and_optional_git_shim
    assets
    %w[Linux macOS].product(%w[X64 ARM64]).each do |os, arch|
      assert_ok(run_script(step("install"), "RUNNER_OS_VALUE" => os, "RUNNER_ARCH_VALUE" => arch))
      path = File.join(@dir, "gthok/bin/gthok")
      assert File.executable?(path)
      assert_includes File.read(@env.fetch("GITHUB_OUTPUT")), "path=#{path}\n"
      assert_includes File.read(@env.fetch("GITHUB_PATH")), "#{File.dirname(path)}\n"
      asset_os = os == "Linux" ? "linux" : "darwin"
      asset_arch = arch == "X64" ? "amd64" : "arm64"
      assert_includes File.read(@env.fetch("TOOL_LOG")), "--pattern gthok_0.0.1_#{asset_os}_#{asset_arch}.tar.gz"
    end
    shim = ACTION.fetch("runs").fetch("steps").last
    assert_equal "inputs.shim == 'true'", shim.fetch("if")
    assert_ok(run_script(shim.fetch("run"), "GTHOK" => File.join(@dir, "gthok/bin/gthok")))
    assert File.symlink?(File.join(@dir, "gthok/shim/git"))
  end

  def test_bad_or_missing_checksum_prevents_installation
    assets
    sums = File.join(@assets, "checksums.txt")
    original = File.read(sums)
    ["", original.gsub(/^[0-9a-f]{64}/, "0" * 64)].each do |contents|
      File.write(sums, contents)
      refute run_script(step("install"), "RUNNER_OS_VALUE" => "Linux", "RUNNER_ARCH_VALUE" => "X64").last.success?
      refute File.exist?(File.join(@dir, "gthok/bin/gthok"))
      refute File.exist?(@env.fetch("GITHUB_PATH"))
    end
  end

  def test_rejects_unsupported_platforms_before_downloading
    [["Windows", "X64"], ["Linux", "ARM"]].each do |os, arch|
      refute run_script(step("install"), "RUNNER_OS_VALUE" => os, "RUNNER_ARCH_VALUE" => arch).last.success?
    end
    refute File.exist?(@env.fetch("TOOL_LOG"))
  end

  def test_release_artifact_contract
    config = YAML.load_file(File.join(ROOT, ".goreleaser.yaml"))
    assert_equal "gthok", config.fetch("project_name")
    build = config.fetch("builds").first
    assert_equal "./cmd/gthok", build.fetch("main")
    assert_equal ["CGO_ENABLED=0"], build.fetch("env")
    assert_equal %w[darwin linux], build.fetch("goos")
    assert_equal %w[amd64 arm64], build.fetch("goarch")
    assert_includes build.fetch("flags"), "-trimpath"
    assert_includes build.fetch("ldflags").join, "main.version={{.Version}}"
    assert_includes build.fetch("ldflags").join, "main.commit={{.FullCommit}}"
    archive = config.fetch("archives").first
    assert_equal ["tar.gz"], archive.fetch("formats")
    assert_equal "{{ .ProjectName }}_{{ .Version }}_{{ .Os }}_{{ .Arch }}", archive.fetch("name_template")
    assert_equal "sha256", config.fetch("checksum").fetch("algorithm")
    assert_equal %w[deb rpm], config.fetch("nfpms").first.fetch("formats")
    assert_equal ["git"], config.fetch("nfpms").first.fetch("dependencies")
  end

  def test_homebrew_renderer_matches_versioned_release_assets
    assets
    render = RELEASE.fetch("jobs").fetch("homebrew").fetch("steps").find { |s| s["name"] == "Render Homebrew formula" }
    assert_ok(run_script(render.fetch("run"), "VERSION" => "0.0.1", "GITHUB_REF_NAME" => "v0.0.1", "GITHUB_REPOSITORY" => "mkusaka/gthok"))
    formula = File.read(File.join(@dir, "gthok.rb"))
    refute_match(/__[A-Z0-9_]+__/, formula)
    %w[linux darwin].product(%w[amd64 arm64]).each do |os, arch|
      assert_includes formula, "gthok_0.0.1_#{os}_#{arch}.tar.gz"
    end
    assert_includes formula, 'depends_on "git"'
  end

  def make_fixture
    FileUtils.cp(File.join(ROOT, "Makefile"), @dir)
    FileUtils.mkdir_p(File.join(@dir, ".github/workflows"))
    FileUtils.cp(File.join(ROOT, ".github/workflows/ci.yml"), File.join(@dir, ".github/workflows"))
    tool("go", 'printf "%s\n" "$*" >> "$TOOL_LOG"')
    tool("git", 'if [ "$1" = diff ]; then exit "${DIFF_STATUS:-0}"; fi')
  end

  def test_fuzz_discovery_and_fixed_iteration_overrides
    make_fixture
    pkg = File.join(@dir, "internal/new")
    FileUtils.mkdir_p(pkg)
    File.write(File.join(pkg, "fuzz_test.go"), "func FuzzNew(f *testing.F) {}\nfunc FuzzTrustLifecycle(f *testing.F) {}\nfunc FuzzLayeredConfig(f *testing.F) {}\nfunc FuzzTrustPathMatch(f *testing.F) {}\n")
    File.write(File.join(pkg, "not_a_fuzz.go"), "func FuzzIgnored() {}\n")
    assert_ok(run_script("make fuzz FUZZTIME=17x", {}, @dir))
    log = File.read(@env.fetch("TOOL_LOG"))
    { "FuzzNew" => "17x", "FuzzTrustLifecycle" => "5000x", "FuzzLayeredConfig" => "20000x", "FuzzTrustPathMatch" => "20000x" }.each do |name, count|
      assert_includes log, "-fuzz ^#{name}$ -fuzztime #{count} ./internal/new"
    end
    refute_includes log, "FuzzIgnored"
    assert_includes assert_ok(run_script("make -n lint", {}, @dir)), "golangci-lint@v2.15.0"
  end

  def test_generation_is_local_and_checks_diffs
    make_fixture
    assert_ok(run_script("make generate-check", { "GIT_SRC" => "" }, @dir))
    assert_equal "generate ./internal/schema\n", File.read(@env.fetch("TOOL_LOG"))
    FileUtils.rm_f(@env.fetch("TOOL_LOG"))
    assert_ok(run_script('make generate-check GIT_SRC="/source/git tree"', {}, @dir))
    assert_includes File.read(@env.fetch("TOOL_LOG")), "run ./internal/registry/gen -git-src /source/git tree -out internal/registry/data"
    refute run_script("make generate-check", { "DIFF_STATUS" => "1" }, @dir).last.success?
    refute run_script("make registry GIT_SRC=", {}, @dir).last.success?
  end

  def test_first_and_subsequent_release_changelog_links
    script = File.read(File.join(ROOT, "scripts/release.sh"))
    awk = script.match(/awk -v tag="\$tag" -v today="\$today" -v prev="\$latest" '(.*?)' CHANGELOG.md/m)[1]
    ["", "v0.0.1"].each do |previous|
      result = Open3.capture3("awk", "-v", "tag=v0.0.2", "-v", "today=2026-11-08", "-v", "prev=#{previous}", awk, File.join(ROOT, "CHANGELOG.md"))
      text = assert_ok(result)
      assert_includes text, "## [Unreleased]\n\n## [v0.0.2] - 2026-11-08"
      url = previous.empty? ? "releases/tag/v0.0.2" : "compare/v0.0.1...v0.0.2"
      assert_includes text, "[v0.0.2]: https://github.com/mkusaka/gthok/#{url}"
    end
    notes = assert_ok(run_script("scripts/changelog-section.sh Unreleased"))
    assert_includes notes, "Initial gthok 0.0.1"
    refute_includes notes, "[Unreleased]:"
  end
end
