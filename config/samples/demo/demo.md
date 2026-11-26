# Demo for Greenhouse Plugin Lifecycle Management

## Show Clusters

```bash
kubectl get -n demo clusters
```

## Describe Clusters
```bash
kubectl describe -n demo cluster rt-eu-de-1
```

## Apply Presets for PodInfo

```bash
kubectl apply -k config/samples/demo/02-presets/podinfo
```
### Show Presets created
```bash
kubectl -n demo get pp
```

### Show Plugins created by the presets
```bash
kubectl -n demo get plugins
```

### Describe Plugin 

```bash
kubectl describe -n demo plugin podinfo-k8s-admin-f-eu-de-1
```

## Apply Source for PodInfo

```bash
kubectl apply -k config/samples/demo/03-sources
```

## Watch the revisions that were created

```bash
kubectl -n demo get revisions -w
```

## Watch the helmreleases to show that the environment rollout is creating helmreleases

```bash
kubectl -n demo get helmreleases -w
```

## Apply Environment for PodInfo

```bash
kubectl apply -k config/samples/demo/04-environments/podinfo
```

## Watch the environments

```bash
kubectl -n demo get environments -w
```

## cd into plugindefinition directory
```bash
cd config/samples/demo/plugindefinition
```

## Push new tag
```bash
make podinfo-push FILE=./podinfo/podinfo-6-15-0.yaml
```