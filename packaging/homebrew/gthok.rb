class Gthok < Formula
  desc "Transparent GitHub CLI wrapper with pre/post hooks and operation normalization"
  homepage "https://github.com/mkusaka/gthok"
  version "__VERSION__"
  license "MIT"

  depends_on "gh"

  on_macos do
    if Hardware::CPU.arm?
      url "__DARWIN_ARM64_URL__"
      sha256 "__DARWIN_ARM64_SHA256__"
    else
      url "__DARWIN_X64_URL__"
      sha256 "__DARWIN_X64_SHA256__"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "__LINUX_ARM64_URL__"
      sha256 "__LINUX_ARM64_SHA256__"
    else
      url "__LINUX_X64_URL__"
      sha256 "__LINUX_X64_SHA256__"
    end
  end

  def install
    bin.install "gthok"
  end

  def caveats
    <<~EOS
      gthok is installed but not active. Route `gh` through it in one of two ways:
        gthok shim install                   # every gh on this machine, incl. scripts and agents
        eval "$(gthok shell-init zsh)"       # only gh typed in your shell (bash/fish also supported)
      The shim creates ~/.local/bin/gh -> #{opt_bin}/gthok (shell rc files are not modified);
      put ~/.local/bin before #{HOMEBREW_PREFIX}/bin in PATH. Undo with `gthok shim uninstall`.
      Without a config gh passes through untouched; create one with:
        gthok config init --scope user
      Then check everything with `gthok doctor`.
      Docs ship with the binary (offline): `gthok docs`, `gthok docs search "<query>"`, `gthok actions`.
    EOS
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gthok version")
  end
end
