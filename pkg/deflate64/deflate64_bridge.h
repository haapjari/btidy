#ifndef BTIDY_DEFLATE64_BRIDGE_H_
#define BTIDY_DEFLATE64_BRIDGE_H_

#include <stddef.h>
#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif

typedef struct btidy_deflate64_state btidy_deflate64_state;

btidy_deflate64_state *btidy_deflate64_new(void);

int btidy_deflate64_init(btidy_deflate64_state *state);

void btidy_deflate64_free(btidy_deflate64_state *state);

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
    int *finished);

const char *btidy_deflate64_error_message(
    const btidy_deflate64_state *state,
    int code);

#ifdef __cplusplus
}
#endif

#endif
