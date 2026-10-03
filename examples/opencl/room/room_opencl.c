#include <CL/cl.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

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

static const char *kernel_source =
"__kernel void step(__global const float *src, __global float *dst) {\n"
"  int x = get_global_id(0); int y = get_global_id(1); int z = get_global_id(2);\n"
"  int i = (z * 64 + y) * 64 + x;\n"
"  // Wide, cold outlet: a 9 x 6 x 6 voxel intake/plenum at the ceiling.\n"
"  if (abs(x - 32) <= 4 && y < 6 && z >= 58) { dst[i] = 0.0f; return; }\n"
"  int xm = x > 0 ? x - 1 : x; int xp = x < 63 ? x + 1 : x;\n"
"  int ym = y > 0 ? y - 1 : y; int yp = y < 63 ? y + 1 : y;\n"
"  int zm = z > 0 ? z - 1 : z; int zp = z < 63 ? z + 1 : z;\n"
"  float c = src[i];\n"
"  float lap = src[(z*64+y)*64+xm] + src[(z*64+y)*64+xp]\n"
"    + src[(z*64+ym)*64+x] + src[(z*64+yp)*64+x]\n"
"    + src[(zm*64+y)*64+x] + src[(zp*64+y)*64+x] - 6.0f*c;\n"
"  // A room-scale circulation loop: +Y at the ceiling and -Y at the floor.\n"
"  float horizontalVelocity = ((float)z - 31.5f) / 32.0f * 0.70f;\n"
"  // The ceiling-mounted air conditioner produces a stronger +Y jet.\n"
"  if (abs(x - 32) <= 5 && z >= 52 && y < 40) horizontalVelocity = 2.00f;\n"
"  int horizontalY = horizontalVelocity >= 0.0f ? ym : yp;\n"
"  float horizontalAdvection = fabs(horizontalVelocity)\n"
"    * (src[(z*64+horizontalY)*64+x] - c);\n"
"  // At the far wall air descends; at the near wall it rises.\n"
"  float verticalVelocity = -((float)y - 31.5f) / 32.0f * 0.90f;\n"
"  int verticalZ = verticalVelocity >= 0.0f ? zm : zp;\n"
"  float verticalAdvection = fabs(verticalVelocity)\n"
"    * (src[(verticalZ*64+y)*64+x] - c);\n"
"  dst[i] = clamp(c + 0.22f * (0.20f * lap\n"
"    + horizontalAdvection + verticalAdvection), 0.0f, 1.0f);\n"
"}\n";

static int check(cl_int error, const char *operation) {
    if (error == CL_SUCCESS) return 0;
    fprintf(stderr, "OpenCL %s failed: %d\n", operation, error);
    return -1;
}

Room *room_open(void) {
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
    size_t source_length = strlen(kernel_source);
    room->program = clCreateProgramWithSource(room->context, 1,
        &kernel_source, &source_length, &error);
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
