using CommandLine;

#nullable enable
namespace GSnapshot
{
    public class Options
    {
        [Option('v', "verbose", Required = false, HelpText = "Set output to verbose messages.")]
        public bool Verbose { get; set; }

        [Option("erasevss", SetName = "rollback", Default = true, Required = false, HelpText = "Erase Windows VSS signature when creating disks.")]
        public bool EraseVSS { get; set; }

        [Option("stop", Required = false, HelpText = "Stop server during snapshots and rollbacks.")]
        public bool Stop { get; set; }

        [Option("start", Required = false, HelpText = "Start server after snapshotting or rolling back.")]
        public bool Start { get; set; }

        [Option("force", Required = false, HelpText = "Force operation.")]
        public bool Force { get; set; }

        [Option("delete", SetName = "commit", Required = false, HelpText = "Automatically delete snapshots when committing.")]
        public bool Delete { get; set; }

        [Option('p', "project", Required = false, HelpText = "Set Google Cloud Platform project ID.")]
        public string? Project { get; set; }

        [Option('r', "region", Required = false, HelpText = "Set region for Compute Engine resources.")]
        public string? Region { get; set; }

        [Option('i', "instance", Required = false, HelpText = "Set Compute Engine instance.")]
        public string? Instance { get; set; }

        [Option('l', "location", SetName = "snapshot", Default = "match", Required = false, HelpText = "Snapshot location (\"eu\", \"us\", \"asia\" or \"match\" to match VM region).")]
        public string Location { get; set; } = "match";

        [Option("sid", Default = 0, Required = false, HelpText = "Snapshot ID (0 = find next available).")]
        public int ID { get; set; }

        [Option("scheduled-id", SetName = "rollback", Required = false, HelpText = "Scheduled snapshot ID.")]
        public string? ScheduledID { get; set; }

        [Option("schedule", Required = false, HelpText = "Snapshot schedule name (use --force to clear other policies).")]
        public string? Schedule { get; set; }

        [Value(0)]
        public string? Command { get; set; }
    }
}