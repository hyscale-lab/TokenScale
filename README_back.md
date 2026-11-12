# LiquidGo

## Docker Compose Usage

### Step 0: Save the model in `./models`

``` bash
docker build -t liquid/save_model -f Dockerfile.savemodel .
docker run -it --gpus all --rm -v ./models:/models liquid/save_model --model-name "facebook/opt-125m" --dtype "float16"
```

Arguments useful:
```
--model-name: str, model name
--dtype: str, data type
--tensor-parallel-size: int, tensor parallel size
```

### Step 1: Start everything

``` bash
docker-compose up --build -d
```

> Note: If you face permission issues, try to set `prometheus/data` as 777. This is because prometheus is running as `nobody` user.

### Step 2: Register model you need (use usertool)

Under `usertools` folder:

``` bash
./usertools demo/model/facebook/opt-125m.json http://localhost:8888/register_model
```

### Step 3: Sent request
``` bash
curl http://localhost:8888/v1/completions \
    -H "Content-Type: application/json" \
    -d '{
        "model": "facebook/opt-125m",
        "prompt": "San Francisco is a",
        "max_tokens": 200,
        "temperature": 0
    }'
```
### Step 4: Backup request

Under `prometheus` folder:
``` bash
./pre-stop.sh
```
Data are saved under `prometheus/data`

### Step 5: Analyze data

First, restore it via point the `--storage.tsdb.path` to the snapshot

Then, run `promtool tsdb dump $SNAPSHOT_PATH > dump.txt`

The format includes: name, tag, value and timestamp

## PBS Usage

### Step 0: Upload the singularity image and model under `~/scratch`

1. Download singularity image from Docker hub

```bash
    qsub scripts/download_liquid_router.pbs
    qsub scripts/download_liquidlet_sif.pbs
    qsub scripts/download_prometheus.pbs
```

2. Upload the scritps

``` bash
rsync -avz ./nscc NSCC:~/
```
We assume all scripts are under `~/nscc` on NSCC machines.

3. Upload the Singularity image. We assume all image are under `~/scratch`

``` bash
liquid_router.sif
liquid_liquidlet.sif
prometheus_v3.2.1.sif
```

4. Upload the transferred model weights. We assume all model weights are under `~/scratch/models`


### Step 1: Edit script

Edit `nscc/sample_config/`. Add models to register

Edit `nscc/sample_loadgen/run.sh`. Change the loadgen script.

Edit `job.sh`. Specify number of nodes you need.

Edit others if you need.

### Step 2: Run

``` bash
qsub job.sh
```

After it finish, all logs are under `~/nscc/logs_PBS_JOBID`. and `~/nscc/GPU_oPBS_JOBID`. Save them. Metrics are also in that folder.
