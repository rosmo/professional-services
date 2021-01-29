using System;
using System.Collections.Generic;

#nullable enable
namespace GSnapshot
{
    class Menu
    {
        private List<string> options;
        private List<int> ids;
        private readonly String title;

        public Menu(string title, Dictionary<int, string> options)
        {
            this.title = title;
            this.options = new List<string>(options.Values);
            this.ids = new List<int>(options.Keys);
        }

        public int? Show()
        {
            int originalCursorTop = Console.CursorTop;
            int selectedOption = 0;
            bool selected = false;
            bool quit = false;
            int fillWidth = Console.WindowWidth - 10;
            ConsoleColor defaultFg = Console.ForegroundColor;
            ConsoleColor defaultBg = Console.BackgroundColor;

            Console.CursorVisible = false;
            Console.Write("\r");
            while (!selected && !quit)
            {
                Console.WriteLine("");
                Console.WriteLine($"  {this.title}");
                Console.WriteLine("");
                for (int i = 0; i < this.options.Count; i++)
                {
                    string index = (1 + i).ToString();
                    Console.Write(selectedOption == i ? " > " : "   ");
                    if (selectedOption == i)
                    {
                        Console.BackgroundColor = ConsoleColor.Gray;
                        Console.ForegroundColor = ConsoleColor.Black;
                    }
                    else
                    {
                        Console.BackgroundColor = defaultBg;
                        Console.ForegroundColor = defaultFg;
                    }
                    Console.Write(String.Format("{0,4}", index) + ". " + this.options[i] + " ");
                    Console.BackgroundColor = defaultBg;
                    Console.ForegroundColor = defaultFg;
                    Console.WriteLine("");
                }
                Console.WriteLine("");
                Console.WriteLine("  Arrow keys to move selection, Enter to select, Esc to cancel.\n");
                Console.SetCursorPosition(0, originalCursorTop - this.options.Count - 6);

                var keyPress = Console.ReadKey(true);
                if (keyPress.Key == ConsoleKey.UpArrow || keyPress.Key == ConsoleKey.W || keyPress.Key == ConsoleKey.K)
                {
                    selectedOption -= 1;
                    if (selectedOption < 0)
                    {
                        selectedOption = this.options.Count - 1;
                    }
                }
                if (keyPress.Key == ConsoleKey.DownArrow || keyPress.Key == ConsoleKey.S || keyPress.Key == ConsoleKey.J)
                {
                    selectedOption += 1;
                    if (selectedOption >= this.options.Count)
                    {
                        selectedOption = 0;
                    }
                }
                if (keyPress.Key == ConsoleKey.Enter)
                {
                    selected = true;
                }
                if (keyPress.Key == ConsoleKey.Escape)
                {
                    quit = true;
                }
            }
            Console.SetCursorPosition(0, originalCursorTop - this.options.Count - 6);
            for (int i = 0; i < this.options.Count + 6; i++)
            {
                Console.WriteLine(String.Format("{0,-" + fillWidth + "}", ""));
            }
            Console.SetCursorPosition(0, originalCursorTop - this.options.Count - 6);

            Console.CursorVisible = true;
            if (quit)
            {
                return null;
            }
            return this.ids[selectedOption];
        }
    }
}