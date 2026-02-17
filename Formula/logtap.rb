# typed: false
# frozen_string_literal: true

class Logtap < Formula
  desc "Ephemeral log mirror for load testing"
  homepage "https://github.com/ppiankov/logtap"
  version "1.0.5"
  license "MIT"

  on_macos do
    on_arm do
      url "https://github.com/ppiankov/logtap/releases/download/v1.0.5/logtap_1.0.5_darwin_arm64.tar.gz"
      sha256 "78b4d15c0f5d4b000e08d9363510fb07abe6284d8d487c680a2175d4635d93f6"
    end
    on_intel do
      url "https://github.com/ppiankov/logtap/releases/download/v1.0.5/logtap_1.0.5_darwin_amd64.tar.gz"
      sha256 "a04796d8b38ebd1c1c9d9655de676e3f4ffae5af88eb7335e5329fe44ac02267"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/ppiankov/logtap/releases/download/v1.0.5/logtap_1.0.5_linux_arm64.tar.gz"
      sha256 "5760113a5055f4e1a6734914b8c2386c144a0e1131a69ae4db252251a3dfa90e"
    end
    on_intel do
      url "https://github.com/ppiankov/logtap/releases/download/v1.0.5/logtap_1.0.5_linux_amd64.tar.gz"
      sha256 "b620976d7a230f0b05eb3250cd7936f0741b9cc49b2dde06442ea4a267d816cb"
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
