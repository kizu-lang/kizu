package native

import (
	"fmt"
	"os/exec"
	"path/filepath"

	"github.com/kizu-lang/kizu/internal/buildcache"
	"github.com/kizu-lang/kizu/internal/stdtarget"
)

// thinLTOCacheDir is the directory under the cache the linker keeps what
// ThinLTO made of each unit in. The linker reads and prunes it itself; it is
// under the cache directory so that a build with an empty cache, the
// bootstrap's second stage say, also starts with no code the linker made
// before.
const thinLTOCacheDir = "thinlto"

// ValidateThinLTO rejects a ThinLTO build the toolchain cannot link before
// anything is compiled for it. ThinLTO optimizes an optimized build across
// its units, so it needs --opt, and the code is made when an executable is
// linked, so it builds one. The linker reads the units as bitcode: Apple's
// ld64 does through the libLTO beside the clang that drives it, and on
// Linux lld does, where the system's GNU ld does not, so a Linux build asks
// for lld and refuses to start without it rather than fail in the link. A
// Windows build is refused: its llvm-mingw linker has not been tried.
func ValidateThinLTO(options Options) error {
	if !options.ThinLTO {
		return nil
	}
	if !options.Opt {
		return fmt.Errorf("native error: --thinlto links an optimized build; add --opt")
	}
	if options.Emit != "exe" {
		return fmt.Errorf("native error: --thinlto makes the code when an executable is linked; " +
			"build --emit exe")
	}
	switch options.Target {
	case stdtarget.NativeDarwin:
		return nil
	case stdtarget.NativeLinux:
		out, err := exec.Command(driver(options), "-fuse-ld=lld", "-Wl,--version").CombinedOutput()
		if err != nil {
			return fmt.Errorf("native error: --thinlto links with lld on Linux, "+
				"and %s -fuse-ld=lld found none: %w\n%s", driver(options), err, out)
		}
		return nil
	default:
		return fmt.Errorf("native error: --thinlto is not supported for %s yet",
			options.Target.OS())
	}
}

// thinLTOCompileFlags are what a ThinLTO build compiles each unit with on
// top of clangFlags: bitcode carrying the summary the link optimizes the
// units together by.
func thinLTOCompileFlags(options Options) []string {
	if !options.ThinLTO {
		return nil
	}
	return []string{"-flto=thin"}
}

// thinLTOCodegenFlags are what a ThinLTO link is asked for besides the
// optimization level: the linker on Linux, and the code generator options
// clangOptimizationFlags gives a compile. ThinLTO makes the code in the
// link, where the flags a unit was compiled with no longer reach, so each
// is handed to the linker's LLVM by its own name. They change the
// executable, so they key it with the other toolchain flags.
//
// The arm64 literal pool is not among them: ld64 makes the code for the
// architecture's baseline processor, not the Apple core a compile tunes
// for, and that processor already loads float constants from the pool,
// while `-mattr` handed to the linker reaches nothing. What does follow the
// baseline is the interleave cap, which is why it is still set.
func thinLTOCodegenFlags(options Options) []string {
	if !options.ThinLTO {
		return nil
	}
	flags := []string{"-flto=thin"}
	if options.Target == stdtarget.NativeLinux {
		flags = append(flags, "-fuse-ld=lld")
	}
	flags = append(flags, "-Wl,-mllvm,-enable-tail-merge=false", "-Wl,-mllvm,-align-loops=32")
	if TargetIsArm64(options.Triple) {
		flags = append(flags, "-Wl,-mllvm,-force-target-max-vector-interleave=4")
	}
	return flags
}

// thinLTOLinkFlags are what a ThinLTO link adds to the command: the code
// generator flags, then where the linker caches what it made of each unit,
// which is not part of what the executable is keyed by because it does not
// change what is made.
func thinLTOLinkFlags(options Options) ([]string, error) {
	if !options.ThinLTO {
		return nil, nil
	}
	cache, err := buildcache.New()
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(cache.Dir, thinLTOCacheDir)
	flags := thinLTOCodegenFlags(options)
	if options.Target == stdtarget.NativeLinux {
		return append(flags, "-Wl,--thinlto-cache-dir="+dir), nil
	}
	return append(flags, "-Wl,-cache_path_lto,"+dir), nil
}
