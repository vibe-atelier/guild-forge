//go:build windows && amd64

// Built from libvpx (see libvpx/LICENSE and libvpx/PATENTS): a fast routine,
// used only when the processor has the instructions it needs.
#pragma GCC target("mmx")
#include "libvpx/vp8/common/x86/idct_blk_mmx.c"
