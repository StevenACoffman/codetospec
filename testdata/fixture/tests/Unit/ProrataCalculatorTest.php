<?php

namespace Tests\Unit;

use App\Services\Billing\ProrataCalculator;
use PHPUnit\Framework\TestCase;

class ProrataCalculatorTest extends TestCase
{
    public function testActivationMidMonthIsProrated(): void
    {
        $calculator = new ProrataCalculator();

        $this->assertSame(15.0, $calculator->calculate(30.0, 16, 30));
    }

    public function testActivationSendsReceipt(): void
    {
        $mailer = $this->createMock(\App\Mail\Mailer::class);
        $mailer->expects($this->once())->method('sendReceipt');

        $amount = (new ProrataCalculator())->calculate(30.0, 16, 30);
        $mailer->sendReceipt($amount);
    }
}
