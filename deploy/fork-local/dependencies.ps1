function Connect-LocalTcpClient([int]$port) {
    $client = [Net.Sockets.TcpClient]::new()
    try {
        if ($client.ConnectAsync('127.0.0.1', $port).Wait(500) -and $client.Connected) { return $client }
    } catch [AggregateException] {
        # An unavailable port faults the connection task.
    }
    $client.Dispose()
}

function Test-LocalPort([int]$port) {
    $client = Connect-LocalTcpClient $port
    if (!$client) { return $false }
    $client.Dispose()
    return $true
}

function Test-LocalRedis {
    $client = Connect-LocalTcpClient 6379
    if (!$client) { return $false }
    try {
        $stream = $client.GetStream()
        $stream.ReadTimeout = 500
        $stream.WriteTimeout = 500
        $ping = [Text.Encoding]::ASCII.GetBytes("PING`r`n")
        $stream.Write($ping, 0, $ping.Length)
        $reader = [IO.StreamReader]::new($stream, [Text.Encoding]::ASCII)
        try { return ($reader.ReadLine() -ceq '+PONG') }
        finally { $reader.Dispose() }
    } catch [IO.IOException] {
        return $false
    } finally {
        $client.Dispose()
    }
}

function Initialize-LocalDependencies {
    if (!(Test-LocalPort 5432)) { throw 'PostgreSQL is not listening on 127.0.0.1:5432.' }
    if (Test-LocalRedis) { return }

    $redisRoot = 'D:\software\Redis'
    $redisBin = Join-Path $redisRoot 'Redis-8.10.3-Windows-x64-cygwin'
    $redisErrorLog = Join-Path $redisRoot 'stderr.log'
    $redisDataLog = Join-Path $redisRoot 'data\redis-local.log'
    $redisProcess = $null
    if (!(Test-LocalPort 6379)) {
        $redisProcess = Start-Process -FilePath (Join-Path $redisBin 'redis-server.exe') -ArgumentList './redis-local.conf' -WorkingDirectory $redisRoot -WindowStyle Hidden -PassThru -RedirectStandardOutput (Join-Path $redisRoot 'stdout.log') -RedirectStandardError $redisErrorLog
        $redisProcess.Id | Set-Content -LiteralPath (Join-Path $redisRoot 'local-process.pid')
    }

    # Redis can bind its port while it is still loading persisted data.
    $timeout = [Diagnostics.Stopwatch]::StartNew()
    while ($timeout.Elapsed.TotalSeconds -lt 30) {
        if (Test-LocalRedis) { return }
        if ($redisProcess) {
            $redisProcess.Refresh()
            if ($redisProcess.HasExited) { throw "Redis exited during startup. Check $redisErrorLog and $redisDataLog." }
        }
        Start-Sleep -Milliseconds 250
    }
    throw "Redis did not answer PING on 127.0.0.1:6379 within 30 seconds. Check $redisErrorLog and $redisDataLog."
}
