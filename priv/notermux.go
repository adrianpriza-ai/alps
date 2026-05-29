//go:build !termux

package priv

func isTermux() bool {
    return os.Getenv("TERMUX_VERSION") != "" ||
        os.Getenv("PREFIX") == "/data/data/com.termux/files/usr"
}
