# TokenScale

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

TokenScale is an autoscaling framework for prefill/decode-disaggregated LLM serving, published at ACM SoCC '26 ([paper](https://doi.org/10.1145/3828161.3857885)). It scales prefillers and decoders based on **Token Velocity**, a leading indicator of backpressure across the prefill, network, and decode stages. It also uses **Convertible Decoders**, decoders that temporarily run prefill work to absorb traffic bursts. In the code, TokenScale is referred to as *TokenPipe* (`tokenpipe`, route policy `TP`).

The system consists of a Go control plane ([`router/`](router/)) with the gateway, load balancer, and autoscalers; a per-node agent ([`liquidlet/`](liquidlet/)) that manages vLLM instances; and an experiment launcher ([`entrypoints/`](entrypoints/)) that deploys everything and sweeps configurations. See [docs/repository_structure.md](docs/repository_structure.md) for a code map and [docs/running_experiments.md](docs/running_experiments.md) for how to run the experiments.

## Referencing our work

If you use TokenScale in your research, please cite our SoCC '26 paper:

```bibtex
@inproceedings{lai2026tokenscale,
  author    = {Lai, Ruiqi and Liu, Hongrui and Lu, Chengzhi and Liu, Zonghao and Cao, Siyu and Shao, Siyang and Zhang, Yixin and Mai, Luo and Ustiugov, Dmitrii},
  title     = {TokenScale: Timely and Accurate Autoscaling for Disaggregated LLM Serving with Token Velocity},
  booktitle = {Proceedings of the 2026 ACM Symposium on Cloud Computing},
  series    = {SoCC '26},
  year      = {2026},
  publisher = {Association for Computing Machinery},
  doi       = {10.1145/3828161.3857885},
  url       = {https://doi.org/10.1145/3828161.3857885}
}
```

## Contributing

Bug reports and feature requests are welcome via [GitHub Issues](https://github.com/hyscale-lab/TokenScale/issues). Changes to `main` go through pull requests; please follow the PR template.

## License and copyright

TokenScale is free software, released under the [MIT License](LICENSE).

Copyright (c) 2026 Nanyang Technological University.

## Maintainers

- Ruiqi Lai ([@lrq619](https://github.com/lrq619))
