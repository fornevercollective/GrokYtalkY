# Homebrew formula (local tap or PR to a personal tap).
#
#   brew install --build-from-source ./Formula/grokytalky.rb
#   # or
#   brew tap fornevercollective/tap   # when published
#   brew install grokytalky
#
class Grokytalky < Formula
  desc "Grok companion dock — walkie, burst video, Strudel, Glyph Matrix mesh"
  homepage "https://github.com/fornevercollective/GrokYtalkY"
  url "https://github.com/fornevercollective/GrokYtalkY/archive/refs/heads/main.tar.gz"
  version "1.9.1"
  license "Apache-2.0"
  head "https://github.com/fornevercollective/GrokYtalkY.git", branch: "main"

  depends_on "go" => :build
  # runtime optional: ffmpeg, ffplay, whisper-cli

  def install
    ldflags = %W[
      -s -w
      -X main.Version=#{version}
      -X main.Commit=homebrew
      -X main.Date=#{time.iso8601}
    ]
    system "go", "build", *std_go_args(ldflags: ldflags.join(" "), output: bin/"grokytalky"), "."
    bin.install_symlink "grokytalky" => "gy"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gy --version")
    assert_match "GrokYtalkY", shell_output("#{bin}/gy version")
    assert_match "burst", shell_output("#{bin}/gy --help")
  end
end
