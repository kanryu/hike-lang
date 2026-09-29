# Embedded Development Workflow for RP2040: Bridging C SDK Primitives with Hike Logic

This guide demonstrates how to build firmware for the Raspberry Pi Pico (RP2040 / ARM Cortex-M0+) by isolating vendor-specific SDK and hardware intrinsics inside clean C wrapper APIs, implementing high-level application logic in Hike, and linking the resulting artifacts using Clang.

> **Status: Experimental**
>
> This document is provisional. Hike currently does not provide a runtime
> for native 32-bit processors such as the RP2040. As a result, the build
> procedure described below may fail until the required 32-bit bare-metal
> runtime support is implemented.

---

## Architecture Overview

Embedded processors often require vendor-specific GCC toolchains, custom intrinsic headers, and complex register access macros (e.g., Pico SDK). Rather than reimplementing these low-level semantics inside `hikec`, the system divides responsibilities between two layers:

* **Hardware Abstraction Layer (C):** Consumes vendor headers, handles low-level peripheral initialization (GPIO, SIO, clocks, interrupts), and exposes clean, flat C APIs without macro dependencies.
* **Application Logic Layer (Hike):** Implements state machines, communication protocols, and control flow using type-safe syntax, and interfaces with the C layer via `cfunc` extern declarations.

```text
[ Pico SDK / Hardware Intrinsics ]
                 │
                 ▼ (Vendor GCC / Clang)
         [ bsp_rp2040.o ]
                 │
                 ├──────────────────────────┐
                 │                          ▼ (Clang / LLD)
         [ app_logic.o  ] ─────────► [ firmware.elf ] ──► [ firmware.uf2 ]
                 ▲
                 │ (hikec -target=cortex-m0)
[ Hike Source (cfunc extern + logic) ]

```

---

## Step 1: Encapsulating Hardware Primitives in C

Create an intermediate C module that wraps the Pico SDK's macro-heavy or inline functions into standard callable functions.

### `bsp_rp2040.h`

```c
#ifndef BSP_RP2040_H
#define BSP_RP2040_H

#include <stdint.h>
#include <stdbool.h>

#ifdef __cplusplus
extern "C" {
#endif

void bsp_init_system(void);
void bsp_gpio_init_output(uint32_t pin);
void bsp_gpio_set(uint32_t pin, bool value);
void bsp_delay_ms(uint32_t ms);

#ifdef __cplusplus
}
#endif

#endif // BSP_RP2040_H

```

### `bsp_rp2040.c`

```c
#include "pico/stdlib.h"
#include "bsp_rp2040.h"

void bsp_init_system(void) {
    stdio_init_all();
}

void bsp_gpio_init_output(uint32_t pin) {
    gpio_init(pin);
    gpio_set_dir(pin, GPIO_OUT);
}

void bsp_gpio_set(uint32_t pin, bool value) {
    gpio_put(pin, value);
}

void bsp_delay_ms(uint32_t ms) {
    sleep_ms(ms);
}

```

---

## Step 2: Hike Application Implementation and FFI

Expose the C wrappers to Hike using `cfunc` extern declarations, then implement the main control loop and state management.

### `main.hike`

```hike
package main

// Declare foreign C API boundaries
cfunc bsp_init_system()
cfunc bsp_gpio_init_output(pin uint32)
cfunc bsp_gpio_set(pin uint32, value bool)
cfunc bsp_delay_ms(ms uint32)

const LED_PIN uint32 = 25

func blink_pattern(count int, on_ms uint32, off_ms uint32) {
    for i := 0; i < count; i++ {
        bsp_gpio_set(LED_PIN, true)
        bsp_delay_ms(on_ms)
        bsp_gpio_set(LED_PIN, false)
        bsp_delay_ms(off_ms)
    }
}

func main() {
    bsp_init_system()
    bsp_gpio_init_output(LED_PIN)

    for {
        // High-level application sequence
        blink_pattern(3, 100, 100)  // Fast blinks
        bsp_delay_ms(500)
        blink_pattern(2, 400, 200)  // Slow blinks
        bsp_delay_ms(1000)
    }
}

```

---

## Step 3: Multi-Toolchain Build and Linking Workflow

Because automatic retain/release is opt-in, compiling without `-retain` omits
the experimental reference-counting operations for strings and slices. The
generated LLVM IR can then be compiled for bare-metal ARMv6-M by Clang. This
does not eliminate all runtime allocation: string and slice operations that
explicitly allocate storage still require the corresponding runtime support.

### 1. Compile C Hardware Wrapper

Compile the C wrapper and Pico SDK components using ARM GCC or Clang targeting ARMv6-M.

```bash
arm-none-eabi-gcc \
    -mcpu=cortex-m0plus \
    -mthumb \
    -O2 \
    -I${PICO_SDK_PATH}/src/common/pico_base/include \
    -I${PICO_SDK_PATH}/src/rp2_common/hardware_gpio/include \
    -I${PICO_SDK_PATH}/src/rp2040/hardware_regs/include \
    -I${PICO_SDK_PATH}/src/rp2_common/pico_stdlib/include \
    -c bsp_rp2040.c -o bsp_rp2040.o

```

### 2. Compile Hike Application

First emit LLVM IR for the Cortex-M0 target. `hikec` produces IR; Clang is
responsible for compiling that IR into an object file.

```bash
# Generates LLVM IR without experimental automatic retain/release
hikec emit-ir -target=cortex-m0 -o app_logic.ll main.hike

# Compile the generated IR into a bare-metal ARMv6-M object file
clang --target=thumbv6m-none-eabi \
    -mcpu=cortex-m0plus \
    -mthumb \
    -c app_logic.ll \
    -o app_logic.o

```

### 3. Link Firmware via Clang

Link the Hike object, C wrapper object, Pico SDK startup runtime, and linker script via Clang.

```bash
clang --target=thumbv6m-none-eabi \
    -mcpu=cortex-m0plus \
    -mthumb \
    -nostdlib \
    -Wl,--gc-sections \
    -Wl,-T,${PICO_SDK_PATH}/src/rp2_common/pico_standard_link/memmap_default.ld \
    bsp_rp2040.o \
    app_logic.o \
    -L${PICO_SDK_BUILD_PATH}/lib -lpico -lhardware \
    -o firmware.elf

```

### 4. Package for Deployment

Convert the linked ELF binary into a UF2 file for flashing onto the RP2040.

```bash
picotool uf2 convert firmware.elf firmware.uf2

```
