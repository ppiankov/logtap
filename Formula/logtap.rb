# typed: false
# frozen_string_literal: true

class Logtap < Formula
  desc "Ephemeral log mirror for load testing"
  homepage "https://github.com/ppiankov/logtap"
  version "1.0.1"
  license "MIT"

  on_macos do
    on_arm do
      url "https://github.com/ppiankov/logtap/releases/download/v1.0.1/logtap_1.0.1_darwin_arm64.tar.gz"
      sha256 "5c0f14dfa85fc6445133d7b1cccc6cc2591f3df71ea560b8a7afc97f9e6efb9e"
    end
    on_intel do
      url "https://github.com/ppiankov/logtap/releases/download/v1.0.1/logtap_1.0.1_darwin_amd64.tar.gz"
      sha256 "9216451058aa62f05c0378e561cbd3e86b79ce632379b1b34d46b8590aa67ee1"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/ppiankov/logtap/releases/download/v1.0.1/logtap_1.0.1_linux_arm64.tar.gz"
      sha256 "6b62fd2f9df8255f62250e748f7fbf41801e6d03e0765358873fa26cc7525ab5"
    end
    on_intel do
      url "https://github.com/ppiankov/logtap/releases/download/v1.0.1/logtap_1.0.1_linux_amd64.tar.gz"
      sha256 "a69198421dd1713726704b223c58eb92afab0628095967abff52861194f65667"
    end
  end

  def install
    bin.install "logtap"

    generate_completions_from_executable(bin/"logtap", "completion")
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/logtap --version")
  end
end
