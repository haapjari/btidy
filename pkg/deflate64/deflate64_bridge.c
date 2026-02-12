#include "deflate64_bridge.h"

#include <limits.h>
#include <stdlib.h>
#include <string.h>

#include "zlib.h"
#include "zutil.h"
#include "infback9.h"

struct btidy_deflate64_state {
  z_stream stream;
  unsigned char *window;

  unsigned char *input;
  size_t input_len;
  size_t input_offset;

  unsigned char *output;
  size_t output_len;
  size_t output_offset;

  int output_overflow;
  int initialized;
};

voidpf zcalloc(voidpf opaque, unsigned items, unsigned size) {
  (void)opaque;
  return calloc(items, size);
}

void zcfree(voidpf opaque, voidpf ptr) {
  (void)opaque;
  free(ptr);
}

static voidpf btidy_zalloc(voidpf opaque, uInt items, uInt size) {
  (void)opaque;
  return calloc(items, size);
}

static void btidy_zfree(voidpf opaque, voidpf ptr) {
  (void)opaque;
  free(ptr);
}

static unsigned btidy_infback9_in(
    void *desc,
    z_const unsigned char **buf) {
  struct btidy_deflate64_state *state =
      (struct btidy_deflate64_state *)desc;

  if (state->input_offset >= state->input_len) {
    *buf = Z_NULL;
    return 0;
  }

  *buf = state->input + state->input_offset;
  size_t remaining = state->input_len - state->input_offset;
  if (remaining > UINT_MAX) {
    remaining = UINT_MAX;
  }

  state->input_offset += remaining;
  return (unsigned)remaining;
}

static int btidy_infback9_out(
    void *desc,
    unsigned char *buf,
    unsigned len) {
  struct btidy_deflate64_state *state =
      (struct btidy_deflate64_state *)desc;

  size_t remaining = state->output_len - state->output_offset;
  size_t to_copy = len;
  if (to_copy > remaining) {
    to_copy = remaining;
  }

  if (to_copy > 0) {
    memcpy(state->output + state->output_offset, buf, to_copy);
  }
  state->output_offset += to_copy;

  if (to_copy < len) {
    state->output_overflow = 1;
    return 1;
  }

  return 0;
}

btidy_deflate64_state *btidy_deflate64_new(void) {
  return (btidy_deflate64_state *)calloc(
      1,
      sizeof(btidy_deflate64_state));
}

int btidy_deflate64_init(btidy_deflate64_state *state) {
  if (state == NULL) {
    return Z_STREAM_ERROR;
  }

  state->window = (unsigned char *)malloc(65536UL);
  if (state->window == NULL) {
    return Z_MEM_ERROR;
  }

  state->stream.zalloc = btidy_zalloc;
  state->stream.zfree = btidy_zfree;
  state->stream.opaque = Z_NULL;

  int ret = inflateBack9Init(&state->stream, state->window);
  if (ret != Z_OK) {
    free(state->window);
    state->window = NULL;
    return ret;
  }

  state->initialized = 1;
  return Z_OK;
}

void btidy_deflate64_free(btidy_deflate64_state *state) {
  if (state == NULL) {
    return;
  }

  if (state->initialized) {
    (void)inflateBack9End(&state->stream);
  }

  if (state->window != NULL) {
    free(state->window);
  }
  free(state);
}

int btidy_deflate64_inflate(
    btidy_deflate64_state *state,
    const uint8_t *input,
    size_t input_len,
    int input_eof,
    uint8_t *output,
    size_t output_len,
    size_t *input_used,
    size_t *output_used,
    int *needs_input,
    int *needs_output,
    int *finished) {
  if (state == NULL || input_used == NULL || output_used == NULL ||
      needs_input == NULL || needs_output == NULL || finished == NULL) {
    return Z_STREAM_ERROR;
  }

  state->input = (unsigned char *)input;
  state->input_len = input_len;
  state->input_offset = 0;

  state->output = output;
  state->output_len = output_len;
  state->output_offset = 0;
  state->output_overflow = 0;

  state->stream.next_in = (Bytef *)input;
  if (input_len > UINT_MAX) {
    state->stream.avail_in = UINT_MAX;
  } else {
    state->stream.avail_in = (uInt)input_len;
  }

  int ret = inflateBack9(
      &state->stream,
      btidy_infback9_in,
      state,
      btidy_infback9_out,
      state);

  *input_used = input_len - (size_t)state->stream.avail_in;
  *output_used = state->output_offset;
  *needs_input = 0;
  *needs_output = 0;
  *finished = 0;

  if (ret == Z_STREAM_END) {
    *finished = 1;
    return Z_OK;
  }

  if (ret == Z_BUF_ERROR) {
    if (state->output_overflow) {
      *needs_output = 1;
      return Z_OK;
    }

    if (*input_used == input_len) {
      *needs_input = 1;
      if (input_eof) {
        return Z_DATA_ERROR;
      }

      return Z_OK;
    }
  }

  return ret;
}

const char *btidy_deflate64_error_message(
    const btidy_deflate64_state *state,
    int code) {
  if (state != NULL && state->stream.msg != Z_NULL) {
    return state->stream.msg;
  }

  switch (code) {
    case Z_DATA_ERROR:
      return "invalid deflate64 stream";
    case Z_MEM_ERROR:
      return "insufficient memory";
    case Z_STREAM_ERROR:
      return "invalid stream state";
    case Z_BUF_ERROR:
      return "insufficient input or output buffer";
    default:
      return "unknown deflate64 error";
  }
}
