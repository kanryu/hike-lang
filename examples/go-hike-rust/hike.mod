module examples_go_hike_rust

hike 1.0

package ./fizzlib {
    target windows {
        hike-object: "none"
        link: "rustlib/target/x86_64-pc-windows-gnullvm/release/libfizzrust.a"
    }
}
