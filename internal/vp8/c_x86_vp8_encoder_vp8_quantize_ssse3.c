//go:build windows && amd64

// Built from libvpx (see libvpx/LICENSE and libvpx/PATENTS): a fast routine,
// used only when the processor has the instructions it needs.
#pragma GCC target("ssse3")
#include "libvpx/vp8/encoder/x86/vp8_quantize_ssse3.c"
