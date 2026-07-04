package main

import "testing"

func TestMainRunsNode(t *testing.T) {
	ran := false
	oldRunNode := runNode
	runNode = func() { ran = true }
	t.Cleanup(func() { runNode = oldRunNode })

	main()

	if !ran {
		t.Fatal("main did not run node")
	}
}
