class Memoryd < Formula
  desc "Local memory daemon and MCP server for coding agents"
  homepage "https://github.com/tomnagengast/agent-memoryd"
  version "{{VERSION_NO_V}}"
  license :cannot_represent

  on_macos do
    depends_on arch: :arm64

    url "https://github.com/tomnagengast/agent-memoryd/releases/download/{{VERSION}}/agent-memoryd_{{VERSION}}_darwin_arm64.tar.gz"
    sha256 "{{DARWIN_ARM64_SHA256}}"
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/tomnagengast/agent-memoryd/releases/download/{{VERSION}}/agent-memoryd_{{VERSION}}_linux_arm64.tar.gz"
      sha256 "{{LINUX_ARM64_SHA256}}"
    elsif Hardware::CPU.intel?
      url "https://github.com/tomnagengast/agent-memoryd/releases/download/{{VERSION}}/agent-memoryd_{{VERSION}}_linux_amd64.tar.gz"
      sha256 "{{LINUX_AMD64_SHA256}}"
    else
      odie "agent-memoryd does not provide a release artifact for this Linux architecture"
    end
  end

  def install
    bin.install "bin/memoryd"
    lib.install Dir["lib/*"]
  end

  def caveats
    <<~EOS
      Run `memoryd init` after installation to create config, Git hooks,
      and the managed daemon service.
    EOS
  end

  test do
    assert_match "memoryd #{version}", shell_output("#{bin}/memoryd --version")
    assert_match "Usage:", shell_output("#{bin}/memoryd --help")
  end
end
