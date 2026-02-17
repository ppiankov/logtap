# typed: false
# frozen_string_literal: true

class Logtap < Formula
  desc "Ephemeral log mirror for load testing"
  homepage "https://github.com/ppiankov/logtap"
  version "1.0.0"
  license "MIT"

  on_macos do
    on_arm do
      url "https://github.com/ppiankov/logtap/releases/download/v1.0.0/logtap_1.0.0_darwin_arm64.tar.gz"
      sha256 "SHA256_DARWIN_ARM64"
    end
    on_intel do
      url "https://github.com/ppiankov/logtap/releases/download/v1.0.0/logtap_1.0.0_darwin_amd64.tar.gz"
      sha256 "SHA256_DARWIN_AMD64"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/ppiankov/logtap/releases/download/v1.0.0/logtap_1.0.0_linux_arm64.tar.gz"
      sha256 "SHA256_LINUX_ARM64"
    end
    on_intel do
      url "https://github.com/ppiankov/logtap/releases/download/v1.0.0/logtap_1.0.0_linux_amd64.tar.gz"
      sha256 "SHA256_LINUX_AMD64"
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
