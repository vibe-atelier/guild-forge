//go:build windows && amd64

// Built from libvpx (see libvpx/LICENSE and libvpx/PATENTS): a fast routine,
// used only when the processor has the instructions it needs.
#pragma GCC target("avx512f,avx512cd,avx512bw,avx512dq,avx512vl")
#include "libvpx/vpx_dsp/x86/sad4d_avx512.c"
