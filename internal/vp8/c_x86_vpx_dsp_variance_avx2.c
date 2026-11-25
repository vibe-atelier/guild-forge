//go:build windows && amd64

// Built from libvpx (see libvpx/LICENSE and libvpx/PATENTS): a fast routine,
// used only when the processor has the instructions it needs.
#pragma GCC target("avx2")
#include "libvpx/vpx_dsp/x86/variance_avx2.c"
