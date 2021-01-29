using System;
using System.Collections.Generic;

#nullable enable
namespace GSnapshot
{
    class AutoCompletionHandler : IAutoCompleteHandler
    {
        private List<string>? options;
        public char[] Separators { get; set; } = new char[] { ' ', '-', '_' };

        public AutoCompletionHandler(Dictionary<string, string> options)
        {
            this.options = new List<string>(options.Values);
        }

        public string[]? GetSuggestions(string text, int index)
        {
            if (text.Length == 0)
            {
                return null;
            }

            List<string> completes = new List<string>();
            if (this.options != null)
            {
                foreach (var label in this.options)
                {
                    if (label.StartsWith(text, StringComparison.CurrentCultureIgnoreCase))
                    {
                        completes.Add(label.Remove(0, index));
                    }
                }
            }
            return completes.ToArray();
        }
    }
}