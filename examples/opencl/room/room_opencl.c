#include <CL/cl.h>
#include <stdio.h>
#include <stdlib.h>

#define N 64
#define CELLS (N * N * N)

typedef struct Room {
    cl_context context;
    cl_command_queue queue;
    cl_program program;
    cl_kernel kernel;
    cl_mem current;
    cl_mem next;
} Room;

void room_close(Room *room);

static int check(cl_int error, const char *operation) {
    if (error == CL_SUCCESS) return 0;
    fprintf(stderr, "OpenCL %s failed: %d\n", operation, error);
    return -1;
}

Room *room_open(const char *kernel_source, int source_length) {
    cl_platform_id platforms[8];
    cl_uint platform_count = 0;
    cl_device_id device = NULL;
    cl_uint device_count = 0;
    cl_int error;

    error = clGetPlatformIDs(8, platforms, &platform_count);
    if (error != CL_SUCCESS) return NULL;
    for (cl_uint i = 0; i < platform_count && !device; i++) {
        if (clGetDeviceIDs(platforms[i], CL_DEVICE_TYPE_GPU, 1, &device,
                           &device_count) == CL_SUCCESS && device_count > 0) break;
    }
    if (!device) {
        fprintf(stderr, "no OpenCL GPU device found\n");
        return NULL;
    }

    Room *room = (Room *)calloc(1, sizeof(Room));
    if (!room) return NULL;
    room->context = clCreateContext(NULL, 1, &device, NULL, NULL, &error);
    if (check(error, "create context")) goto fail;
    room->queue = clCreateCommandQueue(room->context, device, 0, &error);
    if (check(error, "create command queue")) goto fail;
    if (!kernel_source || source_length <= 0) goto fail;
    size_t program_length = (size_t)source_length;
    room->program = clCreateProgramWithSource(room->context, 1,
        &kernel_source, &program_length, &error);
    if (check(error, "create program")) goto fail;
    error = clBuildProgram(room->program, 1, &device, NULL, NULL, NULL);
    if (check(error, "build kernel")) goto fail;
    room->kernel = clCreateKernel(room->program, "step", &error);
    if (check(error, "create kernel")) goto fail;
    room->current = clCreateBuffer(room->context, CL_MEM_READ_WRITE,
        CELLS * sizeof(float), NULL, &error);
    if (check(error, "create current buffer")) goto fail;
    room->next = clCreateBuffer(room->context, CL_MEM_READ_WRITE,
        CELLS * sizeof(float), NULL, &error);
    if (check(error, "create next buffer")) goto fail;

    float *initial = (float *)malloc(CELLS * sizeof(float));
    if (!initial) goto fail;
    for (int i = 0; i < CELLS; i++) initial[i] = 1.0f;
    error = clEnqueueWriteBuffer(room->queue, room->current, CL_TRUE, 0,
        CELLS * sizeof(float), initial, 0, NULL, NULL);
    free(initial);
    if (check(error, "initialize temperature field")) goto fail;
    return room;

fail:
    room_close(room);
    return NULL;
}

int room_step(Room *room) {
    size_t global[3] = {N, N, N};
    cl_int error = clSetKernelArg(room->kernel, 0, sizeof(cl_mem), &room->current);
    if (check(error, "set source buffer")) return -1;
    error = clSetKernelArg(room->kernel, 1, sizeof(cl_mem), &room->next);
    if (check(error, "set destination buffer")) return -1;
    error = clEnqueueNDRangeKernel(room->queue, room->kernel, 3, NULL,
        global, NULL, 0, NULL, NULL);
    if (check(error, "enqueue kernel")) return -1;
    cl_mem swap = room->current;
    room->current = room->next;
    room->next = swap;
    return 0;
}

int room_read(Room *room, float *output, int count) {
    if (!room || !output || count < CELLS) return -1;
    return check(clEnqueueReadBuffer(room->queue, room->current, CL_TRUE, 0,
        CELLS * sizeof(float), output, 0, NULL, NULL), "download result");
}

void room_close(Room *room) {
    if (!room) return;
    if (room->next) clReleaseMemObject(room->next);
    if (room->current) clReleaseMemObject(room->current);
    if (room->kernel) clReleaseKernel(room->kernel);
    if (room->program) clReleaseProgram(room->program);
    if (room->queue) clReleaseCommandQueue(room->queue);
    if (room->context) clReleaseContext(room->context);
    free(room);
}
