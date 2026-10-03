#include "onnxruntime_c_api.h"
#include <stdio.h>
#include <stdlib.h>
#ifdef _WIN32
#include <windows.h>
#endif

typedef struct UpscaleSession {
    const OrtApi *api;
    OrtEnv *env;
    OrtSessionOptions *options;
    OrtSession *session;
    OrtMemoryInfo *memory;
} UpscaleSession;

void upscale_close(void *opaque);

#ifdef _WIN32
static wchar_t *utf8_to_wide(const char *path) {
    int length = MultiByteToWideChar(CP_UTF8, 0, path, -1, NULL, 0);
    if (length <= 0) return NULL;
    wchar_t *wide = (wchar_t *)calloc((size_t)length, sizeof(wchar_t));
    if (!wide) return NULL;
    if (MultiByteToWideChar(CP_UTF8, 0, path, -1, wide, length) <= 0) {
        free(wide);
        return NULL;
    }
    return wide;
}
#endif

static void report_status(const OrtApi *api, OrtStatus *status, const char *where) {
    if (!status) return;
    fprintf(stderr, "ONNX Runtime %s failed: %s\n", where, api->GetErrorMessage(status));
    api->ReleaseStatus(status);
}

void *upscale_init(const char *model_path) {
    const OrtApi *api = OrtGetApiBase()->GetApi(ORT_API_VERSION);
    if (!api) {
        fprintf(stderr, "ONNX Runtime API version is unavailable\n");
        return NULL;
    }
    UpscaleSession *state = (UpscaleSession *)calloc(1, sizeof(UpscaleSession));
    if (!state) return NULL;
    state->api = api;
    OrtStatus *status = api->CreateEnv(ORT_LOGGING_LEVEL_WARNING, "hike-ai", &state->env);
    if (status) goto fail;
    status = api->CreateSessionOptions(&state->options);
    if (status) goto fail;
    status = api->SetIntraOpNumThreads(state->options, 1);
    if (status) goto fail;
    status = api->SetSessionGraphOptimizationLevel(state->options, ORT_ENABLE_ALL);
    if (status) goto fail;
#ifdef _WIN32
    {
        wchar_t *wide_model_path = utf8_to_wide(model_path);
        if (!wide_model_path) {
            fprintf(stderr, "failed to convert model path to UTF-16\n");
            goto fail;
        }
        status = api->CreateSession(state->env, wide_model_path, state->options, &state->session);
        free(wide_model_path);
    }
#else
    status = api->CreateSession(state->env, model_path, state->options, &state->session);
#endif
    if (status) goto fail;
    status = api->CreateCpuMemoryInfo(OrtArenaAllocator, OrtMemTypeDefault, &state->memory);
    if (status) goto fail;
    return state;

fail:
    report_status(api, status, "initialization");
    upscale_close(state);
    return NULL;
}

int upscale_run(void *opaque, const float *input, int width, int height, float *output) {
    UpscaleSession *state = (UpscaleSession *)opaque;
    if (!state || !input || !output || width <= 0 || height <= 0) return -1;
    const OrtApi *api = state->api;
    int64_t input_shape[4] = {1, 3, height, width};
    int64_t output_shape[4] = {1, 3, height * 4, width * 4};
    size_t input_count = (size_t)3 * (size_t)width * (size_t)height;
    size_t output_count = (size_t)3 * (size_t)width * 4 * (size_t)height * 4;
    OrtValue *input_value = NULL;
    OrtValue *output_value = NULL;
    OrtStatus *status = api->CreateTensorWithDataAsOrtValue(
        state->memory, (void *)input, input_count * sizeof(float), input_shape, 4,
        ONNX_TENSOR_ELEMENT_DATA_TYPE_FLOAT, &input_value);
    if (status) goto fail;
    status = api->CreateTensorWithDataAsOrtValue(
        state->memory, output, output_count * sizeof(float), output_shape, 4,
        ONNX_TENSOR_ELEMENT_DATA_TYPE_FLOAT, &output_value);
    if (status) goto fail;
    {
        const char *input_names[] = {"input"};
        const char *output_names[] = {"output"};
        const OrtValue *inputs[] = {input_value};
        OrtValue *outputs[] = {output_value};
        status = api->Run(state->session, NULL, input_names, inputs, 1,
            output_names, 1, outputs);
    }
    if (status) goto fail;
    api->ReleaseValue(output_value);
    api->ReleaseValue(input_value);
    return 0;

fail:
    report_status(api, status, "inference");
    if (output_value) api->ReleaseValue(output_value);
    if (input_value) api->ReleaseValue(input_value);
    return -1;
}

void upscale_close(void *opaque) {
    UpscaleSession *state = (UpscaleSession *)opaque;
    if (!state) return;
    if (state->memory) state->api->ReleaseMemoryInfo(state->memory);
    if (state->session) state->api->ReleaseSession(state->session);
    if (state->options) state->api->ReleaseSessionOptions(state->options);
    if (state->env) state->api->ReleaseEnv(state->env);
    free(state);
}
