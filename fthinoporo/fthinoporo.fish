#!/usr/local/bin/fish
set dest '27dec'
set trigger 'Не ной, марш в детскую'

set page 1
while true
	curl 'http://104.131.74.75/index.php' \
   	   -H 'Connection: keep-alive' \
   	   -H 'Pragma: no-cache' \
   	   -H 'Cache-Control: no-cache' \
   	   -H 'Authorization: Basic aHV5OlBpemRh' \
   	   -H 'Accept: text/html, */*; q=0.01' \
   	   -H 'DNT: 1' \
   	   -H 'X-Requested-With: XMLHttpRequest' \
   	   -H 'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/95.0.4638.69 Safari/537.36' \
   	   -H 'Content-Type: application/x-www-form-urlencoded; charset=UTF-8' \
   	   -H 'Sec-GPC: 1' \
   	   -H 'Origin: http://104.131.74.75' \
   	   -H 'Accept-Language: en-GB,en-US;q=0.9,en;q=0.8' \
   	   --data-raw 'itemsOnPage=1400&currentPage='$page'&dateFrom=&dateTo=&author=&message=&sortOrder=desc&ajaxTime=1638208106&ajaxToken95d4f0915d734ac8a5a825a3549c4bde' \
   	   --compressed \
   	   --insecure > $dest/$page.html
	if string match -q  -- $trigger $dest/$path.html
		break
	end
	set page (math $page + 1)
end
