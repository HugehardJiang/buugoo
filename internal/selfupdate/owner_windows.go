//go:build windows

package selfupdate

func fileOwner(string) (int, int, bool) { return 0, 0, false }
