"""Export 4x-UltraSharp.pth to an ONNX model for the Hike sample."""

import argparse
import re

import torch
from torch import nn


class ResidualDenseBlock5C(nn.Module):
    """The RRDB block used by the public 4x-UltraSharp checkpoint."""

    def __init__(self, nf: int = 64, gc: int = 32) -> None:
        super().__init__()
        self.conv1 = nn.Conv2d(nf, gc, 3, 1, 1, bias=True)
        self.conv2 = nn.Conv2d(nf + gc, gc, 3, 1, 1, bias=True)
        self.conv3 = nn.Conv2d(nf + 2 * gc, gc, 3, 1, 1, bias=True)
        self.conv4 = nn.Conv2d(nf + 3 * gc, gc, 3, 1, 1, bias=True)
        self.conv5 = nn.Conv2d(nf + 4 * gc, nf, 3, 1, 1, bias=True)
        self.activation = nn.LeakyReLU(negative_slope=0.2, inplace=True)

    def forward(self, x: torch.Tensor) -> torch.Tensor:
        x1 = self.activation(self.conv1(x))
        x2 = self.activation(self.conv2(torch.cat((x, x1), dim=1)))
        x3 = self.activation(self.conv3(torch.cat((x, x1, x2), dim=1)))
        x4 = self.activation(self.conv4(torch.cat((x, x1, x2, x3), dim=1)))
        x5 = self.conv5(torch.cat((x, x1, x2, x3, x4), dim=1))
        return x + 0.2 * x5


class RRDB(nn.Module):
    def __init__(self, nf: int = 64, gc: int = 32) -> None:
        super().__init__()
        self.rdb1 = ResidualDenseBlock5C(nf, gc)
        self.rdb2 = ResidualDenseBlock5C(nf, gc)
        self.rdb3 = ResidualDenseBlock5C(nf, gc)

    def forward(self, x: torch.Tensor) -> torch.Tensor:
        return x + 0.2 * self.rdb3(self.rdb2(self.rdb1(x)))


class RRDBNet(nn.Module):
    def __init__(self) -> None:
        super().__init__()
        self.conv_first = nn.Conv2d(3, 64, 3, 1, 1)
        self.RRDB_trunk = nn.Sequential(*(RRDB() for _ in range(23)))
        self.trunk_conv = nn.Conv2d(64, 64, 3, 1, 1)
        self.upconv1 = nn.Conv2d(64, 64, 3, 1, 1)
        self.upconv2 = nn.Conv2d(64, 64, 3, 1, 1)
        self.HRconv = nn.Conv2d(64, 64, 3, 1, 1)
        self.conv_last = nn.Conv2d(64, 3, 3, 1, 1)
        self.activation = nn.LeakyReLU(negative_slope=0.2, inplace=True)

    def forward(self, x: torch.Tensor) -> torch.Tensor:
        feature = self.conv_first(x)
        trunk = self.trunk_conv(self.RRDB_trunk(feature))
        feature = feature + trunk
        feature = self.activation(self.upconv1(nn.functional.interpolate(
            feature, scale_factor=2, mode="nearest")))
        feature = self.activation(self.upconv2(nn.functional.interpolate(
            feature, scale_factor=2, mode="nearest")))
        return self.conv_last(self.activation(self.HRconv(feature)))


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--input", default="4x-UltraSharp.pth")
    parser.add_argument("--output", default="4x-UltraSharp.onnx")
    parser.add_argument("--size", type=int, default=64)
    args = parser.parse_args()

    checkpoint = torch.load(args.input, map_location="cpu", weights_only=False)
    state = checkpoint.get("params_ema", checkpoint.get("params", checkpoint))
    model = RRDBNet()
    # 4x-UltraSharp is distributed with the original ESRGAN sequential
    # module names (model.1.sub.0.RDB1...), while the local implementation
    # uses descriptive RRDBNet names. Translate the keys without changing any
    # tensor data or model layers.
    translated = {}
    for key, value in state.items():
        direct = {
            "model.0": "conv_first",
            "model.1.sub.23": "trunk_conv",
            "model.3": "upconv1",
            "model.6": "upconv2",
            "model.8": "HRconv",
            "model.10": "conv_last",
        }
        match = re.match(r"model\.1\.sub\.(\d+)\.RDB([123])\.conv([1-5])\.0\.(weight|bias)$", key)
        if match:
            block, rdb, conv, parameter = match.groups()
            new_key = f"RRDB_trunk.{block}.rdb{rdb}.conv{conv}.{parameter}"
        else:
            new_key = key
            for old, new in direct.items():
                if key.startswith(old + "."):
                    new_key = new + key[len(old):]
                    break
        translated[new_key] = value
    model.load_state_dict(translated, strict=True)
    model.eval()
    sample = torch.zeros(1, 3, args.size, args.size, dtype=torch.float32)
    torch.onnx.export(
        model,
        sample,
        args.output,
        input_names=["input"],
        output_names=["output"],
        dynamic_axes={"input": {2: "height", 3: "width"}, "output": {2: "out_height", 3: "out_width"}},
        opset_version=17,
        do_constant_folding=True,
        dynamo=False,
    )
    print(f"wrote {args.output}")


if __name__ == "__main__":
    main()
