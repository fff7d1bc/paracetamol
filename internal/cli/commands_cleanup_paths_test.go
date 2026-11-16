package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"paracetamol/internal/identity"
)

func TestCleanupRejectsReplacedAncestorDuringConfirmation(t *testing.T) {
	for _, scope := range []string{"data", "caches", "downloads", "build-cache"} {
		for _, replacement := range []string{"symlink", "directory"} {
			t.Run(scope+"/"+replacement, func(t *testing.T) {
				base := gatewayOwnershipHome(t)
				parent := filepath.Join(base, "selected")
				root := filepath.Join(parent, "library")
				target := root
				switch scope {
				case "caches":
					target = filepath.Join(root, "apps", "llama-cpp", "cache")
				case "downloads":
					target = filepath.Join(root, "staging")
				case "build-cache":
					target = filepath.Join(root, identity.StateNamespace, "build")
				}
				relative, err := filepath.Rel(parent, target)
				if err != nil {
					t.Fatal(err)
				}
				outside := filepath.Join(base, "unrelated")
				writeOwnershipSentinel(t, filepath.Join(target, "original"))
				writeOwnershipSentinel(t, filepath.Join(outside, relative, "unrelated"))
				app, stdout, _ := testApp(t, &commandRunner{run: gatewayOwnershipPodman})
				app.Environment = map[string]string{"HOME": base, "XDG_CACHE_HOME": root}
				app.Stdin = preflightTerminalReader{Reader: strings.NewReader("yes\n")}
				prompted := false
				app.Stdout = ownershipHookWriter{Writer: stdout, hook: func(value string) {
					if prompted || !strings.Contains(value, "Proceed with") {
						return
					}
					prompted = true
					if err := os.Rename(parent, parent+"-saved"); err != nil {
						t.Fatal(err)
					}
					if replacement == "symlink" {
						err = os.Symlink(outside, parent)
					} else {
						err = os.Rename(outside, parent)
					}
					if err != nil {
						t.Fatal(err)
					}
				}}
				args := []string{scope}
				if scope != "build-cache" {
					args = append(args, "--data-dir", root)
				}
				if err := app.commandCleanup(args); err == nil {
					t.Fatal("cleanup accepted a replaced ancestor")
				}
				if !prompted || strings.Contains(stdout.String(), "Removed:") {
					t.Fatalf("unexpected cleanup output: %s", stdout)
				}
				for _, path := range []string{filepath.Join(parent+"-saved", relative, "original"), filepath.Join(parent, relative, "unrelated")} {
					if _, err := os.Stat(path); err != nil {
						t.Fatalf("unapproved deletion at %s: %v", path, err)
					}
				}
			})
		}
	}
}

func TestCleanupRevalidatesCompleteDirectoryPlanBeforeRemoval(t *testing.T) {
	for _, replacement := range []string{"directory", "file", "symlink", "absent"} {
		t.Run(replacement, func(t *testing.T) {
			base := t.TempDir()
			first, last := filepath.Join(base, "first"), filepath.Join(base, "last")
			writeOwnershipSentinel(t, filepath.Join(first, "first"))
			writeOwnershipSentinel(t, filepath.Join(last, "last"))
			outside := filepath.Join(base, "outside")
			writeOwnershipSentinel(t, filepath.Join(outside, "outside"))
			app, stdout, _ := testApp(t, &commandRunner{})
			app.Stdin = preflightTerminalReader{Reader: strings.NewReader("yes\n")}
			prompted := false
			app.Stdout = ownershipHookWriter{Writer: stdout, hook: func(value string) {
				if prompted || !strings.Contains(value, "Proceed with") {
					return
				}
				prompted = true
				if err := os.Rename(last, last+"-saved"); err != nil {
					t.Fatal(err)
				}
				var err error
				switch replacement {
				case "directory":
					writeOwnershipSentinel(t, filepath.Join(last, "new"))
				case "file":
					err = os.WriteFile(last, []byte("new"), 0o600)
				case "symlink":
					err = os.Symlink(outside, last)
				}
				if err != nil {
					t.Fatal(err)
				}
			}}
			if err := app.cleanupPaths("caches", []string{first, last}, false, false); err == nil {
				t.Fatal("cleanup accepted a replaced target")
			}
			if !prompted || strings.Contains(stdout.String(), "Removed:") {
				t.Fatalf("unexpected cleanup output: %s", stdout)
			}
			for _, path := range []string{filepath.Join(first, "first"), filepath.Join(last+"-saved", "last"), filepath.Join(outside, "outside")} {
				if _, err := os.Stat(path); err != nil {
					t.Fatalf("complete plan was not validated before mutation: %v", err)
				}
			}
		})
	}
}

func TestCleanupTraversalRemainsBoundAfterValidation(t *testing.T) {
	base := t.TempDir()
	parent := filepath.Join(base, "selected")
	path := filepath.Join(parent, "library")
	writeOwnershipSentinel(t, filepath.Join(path, "original"))
	outside := filepath.Join(base, "unrelated")
	writeOwnershipSentinel(t, filepath.Join(outside, "library", "unrelated"))
	target, err := planCleanupDirectory(path)
	if err != nil {
		t.Fatal(err)
	}
	defer target.close()
	if err := target.validate(); err != nil {
		t.Fatal(err)
	}
	// Model a rename immediately after validation, not just during the prompt.
	if err := os.Rename(parent, parent+"-saved"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, parent); err != nil {
		t.Fatal(err)
	}
	if err := target.clearContents(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(outside, "library", "unrelated")); err != nil {
		t.Fatalf("bound traversal followed replacement: %v", err)
	}
	if _, err := os.Stat(filepath.Join(parent+"-saved", "library", "original")); !os.IsNotExist(err) {
		t.Fatalf("bound traversal did not use the captured directory: %v", err)
	}
	if err := target.validate(); err == nil {
		t.Fatal("replaced path passed final identity validation")
	}
}

func TestCleanupDirectoryRejectsSymlinkedPlanAndPreservesLinkTargets(t *testing.T) {
	base := t.TempDir()
	path := filepath.Join(base, "selected")
	outside := filepath.Join(base, "outside")
	writeOwnershipSentinel(t, filepath.Join(path, "nested", "inside"))
	writeOwnershipSentinel(t, filepath.Join(outside, "outside"))
	if err := os.Symlink(outside, filepath.Join(path, "link")); err != nil {
		t.Fatal(err)
	}
	for index := range 300 {
		writeOwnershipSentinel(t, filepath.Join(path, fmt.Sprintf("entry-%03d", index)))
	}
	alias := filepath.Join(base, "alias")
	if err := os.Symlink(path, alias); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{alias, filepath.Join(alias, "nested")} {
		if target, err := planCleanupDirectory(value); err == nil {
			target.close()
			t.Fatalf("accepted symlinked cleanup plan: %s", value)
		}
	}
	target, err := planCleanupDirectory(path)
	if err != nil {
		t.Fatal(err)
	}
	defer target.close()
	if err := target.remove(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("cleanup left selected directory: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outside, "outside")); err != nil {
		t.Fatalf("cleanup followed content symlink: %v", err)
	}
}

func TestCleanupFinalRemovalCannotUnlinkReplacementFileOrLink(t *testing.T) {
	for _, replacement := range []string{"file", "symlink", "nonempty-directory"} {
		t.Run(replacement, func(t *testing.T) {
			base := t.TempDir()
			path := filepath.Join(base, "selected")
			if err := os.Mkdir(path, 0o700); err != nil {
				t.Fatal(err)
			}
			target, err := planCleanupDirectory(path)
			if err != nil {
				t.Fatal(err)
			}
			defer target.close()
			if err := target.validate(); err != nil {
				t.Fatal(err)
			}
			// Model replacement after the last identity check. The final
			// syscall must never unlink a new file/link or recurse into a tree.
			if err := os.Rename(path, path+"-saved"); err != nil {
				t.Fatal(err)
			}
			outside := filepath.Join(base, "outside")
			writeOwnershipSentinel(t, filepath.Join(outside, "unrelated"))
			switch replacement {
			case "file":
				err = os.WriteFile(path, []byte("unrelated"), 0o600)
			case "symlink":
				err = os.Symlink(outside, path)
			case "nonempty-directory":
				writeOwnershipSentinel(t, filepath.Join(path, "unrelated"))
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := removeEmptyCleanupDirectory(target.parent, target.name); err == nil {
				t.Fatal("removed a replacement that was not an empty directory")
			}
			if _, err := os.Lstat(path); err != nil {
				t.Fatalf("replacement did not survive: %v", err)
			}
			if _, err := os.Stat(filepath.Join(outside, "unrelated")); err != nil {
				t.Fatalf("followed replacement symlink: %v", err)
			}
		})
	}
}
