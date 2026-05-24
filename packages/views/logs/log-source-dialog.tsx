"use client";

import { useEffect, useState } from "react";
import { toast } from "sonner";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { Switch } from "@multica/ui/components/ui/switch";
import type { LogProvider, LogSource } from "@multica/core/types/log-source";
import { useCreateLogSource, useUpdateLogSource } from "@multica/core/log-source/mutations";

interface LogSourceDialogProps {
  open: boolean;
  onOpenChange: (v: boolean) => void;
  wsId: string;
  existingSource?: LogSource;
}

interface CloudwatchConfig {
  region: string;
  log_group_name: string;
  filter_pattern: string;
  access_key_id: string;
  secret_access_key: string;
}

interface S3Config {
  region: string;
  bucket: string;
  prefix: string;
  access_key_id: string;
  secret_access_key: string;
}

function getStringConfig(config: Record<string, unknown>, key: string): string {
  const val = config[key];
  return typeof val === "string" ? val : "";
}

export function LogSourceDialog({ open, onOpenChange, wsId, existingSource }: LogSourceDialogProps) {
  const isEdit = !!existingSource;

  const [name, setName] = useState("");
  const [provider, setProvider] = useState<LogProvider>("cloudwatch");
  const [enabled, setEnabled] = useState(true);
  const [pollInterval, setPollInterval] = useState(60);
  const [autoCreate, setAutoCreate] = useState(false);

  // CloudWatch config
  const [cwRegion, setCwRegion] = useState("");
  const [cwLogGroup, setCwLogGroup] = useState("");
  const [cwFilterPattern, setCwFilterPattern] = useState("ERROR");
  const [cwAccessKeyId, setCwAccessKeyId] = useState("");
  const [cwSecretAccessKey, setCwSecretAccessKey] = useState("");

  // S3 config
  const [s3Region, setS3Region] = useState("");
  const [s3Bucket, setS3Bucket] = useState("");
  const [s3Prefix, setS3Prefix] = useState("");
  const [s3AccessKeyId, setS3AccessKeyId] = useState("");
  const [s3SecretAccessKey, setS3SecretAccessKey] = useState("");

  useEffect(() => {
    if (existingSource) {
      setName(existingSource.name);
      setProvider(existingSource.provider);
      setEnabled(existingSource.enabled);
      setPollInterval(existingSource.poll_interval_minutes);
      setAutoCreate(existingSource.auto_create_issues);

      const cfg = existingSource.config;
      if (existingSource.provider === "cloudwatch") {
        setCwRegion(getStringConfig(cfg, "region"));
        setCwLogGroup(getStringConfig(cfg, "log_group_name"));
        setCwFilterPattern(getStringConfig(cfg, "filter_pattern") || "ERROR");
        setCwAccessKeyId(getStringConfig(cfg, "access_key_id"));
        setCwSecretAccessKey("");
      } else {
        setS3Region(getStringConfig(cfg, "region"));
        setS3Bucket(getStringConfig(cfg, "bucket"));
        setS3Prefix(getStringConfig(cfg, "prefix"));
        setS3AccessKeyId(getStringConfig(cfg, "access_key_id"));
        setS3SecretAccessKey("");
      }
    } else {
      setName("");
      setProvider("cloudwatch");
      setEnabled(true);
      setPollInterval(60);
      setAutoCreate(false);
      setCwRegion("");
      setCwLogGroup("");
      setCwFilterPattern("ERROR");
      setCwAccessKeyId("");
      setCwSecretAccessKey("");
      setS3Region("");
      setS3Bucket("");
      setS3Prefix("");
      setS3AccessKeyId("");
      setS3SecretAccessKey("");
    }
  }, [existingSource, open]);

  const createSource = useCreateLogSource(wsId);
  const updateSource = useUpdateLogSource(wsId);

  const isPending = createSource.isPending || updateSource.isPending;

  function buildConfig(): Record<string, unknown> {
    if (provider === "cloudwatch") {
      const cfg: CloudwatchConfig = {
        region: cwRegion.trim(),
        log_group_name: cwLogGroup.trim(),
        filter_pattern: cwFilterPattern.trim(),
        access_key_id: cwAccessKeyId.trim(),
        secret_access_key: cwSecretAccessKey.trim(),
      };
      return cfg as unknown as Record<string, unknown>;
    } else {
      const cfg: S3Config = {
        region: s3Region.trim(),
        bucket: s3Bucket.trim(),
        prefix: s3Prefix.trim(),
        access_key_id: s3AccessKeyId.trim(),
        secret_access_key: s3SecretAccessKey.trim(),
      };
      return cfg as unknown as Record<string, unknown>;
    }
  }

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    try {
      if (isEdit && existingSource) {
        await updateSource.mutateAsync({
          id: existingSource.id,
          input: {
            name: name.trim(),
            config: buildConfig(),
            enabled,
            poll_interval_minutes: pollInterval,
            auto_create_issues: autoCreate,
          },
        });
        toast.success("Log source updated");
      } else {
        await createSource.mutateAsync({
          name: name.trim(),
          provider,
          config: buildConfig(),
          enabled,
          poll_interval_minutes: pollInterval,
          auto_create_issues: autoCreate,
        });
        toast.success("Log source added");
      }
      onOpenChange(false);
    } catch {
      toast.error(isEdit ? "Failed to update log source" : "Failed to add log source");
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle>{isEdit ? "Edit log source" : "Add log source"}</DialogTitle>
        </DialogHeader>

        <form onSubmit={handleSubmit} className="space-y-4">
          <div className="space-y-1.5">
            <Label htmlFor="ls-name">Name</Label>
            <Input
              id="ls-name"
              className="h-8 text-xs"
              value={name}
              onChange={(e) => setName(e.target.value)}
              required
            />
          </div>

          {!isEdit && (
            <div className="space-y-1.5">
              <Label htmlFor="ls-provider">Provider</Label>
              <Select value={provider} onValueChange={(v) => setProvider(v as LogProvider)}>
                <SelectTrigger id="ls-provider" className="h-8 text-xs">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="cloudwatch">CloudWatch</SelectItem>
                  <SelectItem value="s3">S3</SelectItem>
                </SelectContent>
              </Select>
            </div>
          )}

          {provider === "cloudwatch" ? (
            <>
              <div className="space-y-1.5">
                <Label htmlFor="ls-cw-region">AWS Region</Label>
                <Input
                  id="ls-cw-region"
                  className="h-8 text-xs"
                  placeholder="us-east-1"
                  value={cwRegion}
                  onChange={(e) => setCwRegion(e.target.value)}
                  required
                />
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="ls-cw-log-group">Log group name</Label>
                <Input
                  id="ls-cw-log-group"
                  className="h-8 text-xs"
                  placeholder="/aws/lambda/my-function"
                  value={cwLogGroup}
                  onChange={(e) => setCwLogGroup(e.target.value)}
                  required
                />
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="ls-cw-filter">Filter pattern</Label>
                <Input
                  id="ls-cw-filter"
                  className="h-8 text-xs"
                  placeholder="ERROR"
                  value={cwFilterPattern}
                  onChange={(e) => setCwFilterPattern(e.target.value)}
                />
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="ls-cw-key-id">Access key ID</Label>
                <Input
                  id="ls-cw-key-id"
                  className="h-8 text-xs font-mono"
                  value={cwAccessKeyId}
                  onChange={(e) => setCwAccessKeyId(e.target.value)}
                />
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="ls-cw-secret">Secret access key</Label>
                <Input
                  id="ls-cw-secret"
                  type="password"
                  className="h-8 text-xs font-mono"
                  placeholder={isEdit ? "Leave blank to keep existing" : ""}
                  value={cwSecretAccessKey}
                  onChange={(e) => setCwSecretAccessKey(e.target.value)}
                />
              </div>
            </>
          ) : (
            <>
              <div className="space-y-1.5">
                <Label htmlFor="ls-s3-region">AWS Region</Label>
                <Input
                  id="ls-s3-region"
                  className="h-8 text-xs"
                  placeholder="us-east-1"
                  value={s3Region}
                  onChange={(e) => setS3Region(e.target.value)}
                  required
                />
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="ls-s3-bucket">S3 bucket</Label>
                <Input
                  id="ls-s3-bucket"
                  className="h-8 text-xs"
                  placeholder="my-logs-bucket"
                  value={s3Bucket}
                  onChange={(e) => setS3Bucket(e.target.value)}
                  required
                />
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="ls-s3-prefix">Object prefix (optional)</Label>
                <Input
                  id="ls-s3-prefix"
                  className="h-8 text-xs"
                  placeholder="logs/errors/"
                  value={s3Prefix}
                  onChange={(e) => setS3Prefix(e.target.value)}
                />
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="ls-s3-key-id">Access key ID</Label>
                <Input
                  id="ls-s3-key-id"
                  className="h-8 text-xs font-mono"
                  value={s3AccessKeyId}
                  onChange={(e) => setS3AccessKeyId(e.target.value)}
                />
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="ls-s3-secret">Secret access key</Label>
                <Input
                  id="ls-s3-secret"
                  type="password"
                  className="h-8 text-xs font-mono"
                  placeholder={isEdit ? "Leave blank to keep existing" : ""}
                  value={s3SecretAccessKey}
                  onChange={(e) => setS3SecretAccessKey(e.target.value)}
                />
              </div>
            </>
          )}

          <div className="space-y-1.5">
            <Label htmlFor="ls-interval">Poll interval (minutes)</Label>
            <Input
              id="ls-interval"
              type="number"
              className="h-8 text-xs"
              min={1}
              value={pollInterval}
              onChange={(e) => setPollInterval(parseInt(e.target.value, 10) || 60)}
            />
          </div>

          <div className="flex items-center justify-between">
            <Label htmlFor="ls-auto-create">Auto-create issues</Label>
            <Switch id="ls-auto-create" checked={autoCreate} onCheckedChange={setAutoCreate} />
          </div>

          <div className="flex items-center justify-between">
            <Label htmlFor="ls-enabled">Enabled</Label>
            <Switch id="ls-enabled" checked={enabled} onCheckedChange={setEnabled} />
          </div>

          <div className="flex justify-end gap-2 pt-2">
            <Button type="button" size="sm" variant="outline" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button type="submit" size="sm" disabled={isPending || !name.trim()}>
              {isPending ? "Saving..." : "Save"}
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}
