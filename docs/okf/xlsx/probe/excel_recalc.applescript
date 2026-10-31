-- usage: osascript excel_recalc.applescript <src.xlsx> <dst.xlsx>   (absolute POSIX paths)
-- Opens src in Microsoft Excel, forces a full calculation, saves a copy as dst, closes without touching src.
on run argv
	set srcPath to item 1 of argv
	set dstPath to item 2 of argv
	-- After Save As the workbook is renamed and the original reference goes stale, so close by the new name.
	set dstName to do shell script "basename " & quoted form of dstPath
	tell application "Microsoft Excel"
		set display alerts to false
		try
			set wb to open workbook workbook file name srcPath update links do not update links
			calculate full
			save workbook as wb filename dstPath file format Excel XML file format
			-- Close INSIDE the try. A workbook left open here is reused by the next recalculation
			-- (check.py runs several per task) and makes every later recalc fail.
			close workbook dstName saving no
		on error errMsg number errNum
			-- Best effort: close by the saved name (the original reference is stale after Save As),
			-- then by the reference itself. Each close failure is ignored on purpose so the error
			-- that actually happened is the one reported; a stranded workbook must never mask it.
			try
				close workbook dstName saving no
			end try
			try
				close wb saving no
			end try
			set display alerts to true
			error errMsg number errNum
		end try
		-- Do not leave the user's Excel silently suppressing every alert after a probe run.
		set display alerts to true
	end tell
end run
